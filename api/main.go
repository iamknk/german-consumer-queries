package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// ====== Schema types ======
type Dates struct {
	Checkin  string `json:"checkin"`
	Checkout string `json:"checkout"`
}
type Guests struct {
	Adults   int `json:"adults"`
	Children int `json:"children"`
}
type UiFilters struct {
	Meals             []string `json:"meals"`
	Ratings           []string `json:"ratings"`
	HotelTypes        []string `json:"hotelTypes"`
	Hotelfacilities   []string `json:"hotelfacilities"`
	Poolbeach         []string `json:"poolbeach"`
	DistanceBeach     []string `json:"distanceBeach"`
	TravelGroup       []string `json:"travelGroup"`
	Stars             []string `json:"stars"`
	Wellness          []string `json:"wellness"`
	ReferenceDistance []string `json:"reference_distance_max"`
	Flex              []string `json:"flex"`
	Children          []string `json:"children"`
	Parking           []string `json:"parking"`
	Freetime          []string `json:"freetime"`
	Certifications    []string `json:"certifications"`
	Hotelthemes       []string `json:"hotelthemes"`
	HotelBrand        []string `json:"hotelBrand"`
	Hotelinformation  []string `json:"hotelinformation"`
}
type ParseResponse struct {
	Location            string    `json:"location"`
	Dates               Dates     `json:"dates"`
	Guests              Guests    `json:"guests"`
	PriceMaxEUR         float64   `json:"price_max_eur"`
	StarsMin            int       `json:"stars_min"`
	RatingMin           float64   `json:"rating_min"`
	FamilyFriendly      bool      `json:"family_friendly"`
	UiFilters           UiFilters `json:"ui_filters"`
	UnsupportedCriteria []string  `json:"unsupported_criteria"`
}

func (p *ParseResponse) Validate() error {
	if p.StarsMin < 0 || p.StarsMin > 5 {
		return errors.New("stars_min must be 0..5")
	}
	if p.RatingMin < 0 || p.RatingMin > 10 {
		return errors.New("rating_min must be 0..10")
	}
	// Roundtrip to reject unknowns
	b, _ := json.Marshal(p)
	var strict ParseResponse
	dec := json.NewDecoder(bytesReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(&strict)
}

// tiny readers
type byteReader struct{ b []byte }
func bytesReader(b []byte) *byteReader { return &byteReader{b: b} }
func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 { return 0, io.EOF }
	n := copy(p, r.b); r.b = r.b[n:]; return n, nil
}
type strReader struct{ s string }
func stringsReader(s string) *strReader { return &strReader{s: s} }
func (r *strReader) Read(p []byte) (int, error) {
	if len(r.s) == 0 { return 0, io.EOF }
	n := copy(p, r.s); r.s = r.s[n:]; return n, nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type parseInput struct {
	Query string `json:"query_de"`
}

func parseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input parseInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if input.Query == "" {
		http.Error(w, "query_de is required", http.StatusBadRequest)
		return
	}

	_ = godotenv.Load() // load .env if present

	cli, err := NewOpenAIClient()
	if err != nil {
		http.Error(w, "OpenAI client error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build system prompt (allow overrides on disk)
	systemPrompt := defaultSystemPrompt
	if b, err := os.ReadFile("prompt/system.txt"); err == nil {
		systemPrompt = string(b)
	}
	if b, err := os.ReadFile("prompt/examples.json"); err == nil {
		systemPrompt += "\n\nBeispiele (nur zur Steuerung, nicht ausgeben):\n" + string(b)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	rawJSON, err := cli.CompleteJSON(ctx, systemPrompt, input.Query)
	if err != nil {
		http.Error(w, "LLM error: "+err.Error(), http.StatusBadGateway)
		return
	}

	var out ParseResponse
	dec := json.NewDecoder(stringsReader(rawJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		http.Error(w, "schema violation: "+err.Error()+"\nmodel_output="+rawJSON, http.StatusBadRequest)
		return
	}
	if err := out.Validate(); err != nil {
		http.Error(w, "validation error: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func main() {
	_ = godotenv.Load()
	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" { addr = ":" + p }

	mux := http.NewServeMux()
	mux.Handle("/v1/parse", corsMiddleware(http.HandlerFunc(parseHandler)))

	log.Println("Server on " + addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
