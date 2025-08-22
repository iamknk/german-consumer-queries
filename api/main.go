package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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

// Basic range checks
func (p *ParseResponse) Validate() error {
	if p.StarsMin < 0 || p.StarsMin > 5 {
		return errors.New("stars_min must be 0..5")
	}
	if p.RatingMin < 0 || p.RatingMin > 10 {
		return errors.New("rating_min must be 0..10")
	}
	if p.Guests.Adults < 0 || p.Guests.Children < 0 {
		return errors.New("guest counts cannot be negative")
	}
	return nil
}

// A safe default prompt if prompt/system.txt isn't present
const defaultSystemPrompt = `Du bist ein Parser. Analysiere eine deutsche Hotelsuchanfrage
und gib ausschließlich ein einziges JSON-Objekt gemäß diesem Schema aus (keine Erklärungen):
{
  "location": string,
  "dates": { "checkin": string, "checkout": string },
  "guests": { "adults": number, "children": number },
  "price_max_eur": number,
  "stars_min": number,
  "rating_min": number,
  "family_friendly": boolean,
  "ui_filters": {
    "meals": string[],
    "ratings": string[],
    "hotelTypes": string[],
    "hotelfacilities": string[],
    "poolbeach": string[],
    "distanceBeach": string[],
    "travelGroup": string[],
    "stars": string[],
    "wellness": string[],
    "reference_distance_max": string[],
    "flex": string[],
    "children": string[],
    "parking": string[],
    "freetime": string[],
    "certifications": string[],
    "hotelthemes": string[],
    "hotelBrand": string[],
    "hotelinformation": string[]
  },
  "unsupported_criteria": string[]
}
  
Regeln:
- Preis als EUR-Zahl in price_max_eur (z. B. „unter 150€“ → 150). Keine Buckets.
- Synonyme mappen: WLAN→hotelfacilities.free_hotel_wifi; Frühstück→meals.breakfast; All-inclusive/AI→meals.only_all_inclusive (+hotelthemes.allInclusiveHotel wenn Thema); Wellness/Spa→wellness.spa; Innenpool→poolbeach.heated_pool; Außenpool→poolbeach.pool; „am Strand“→distanceBeach:["500"]; Adults-only→travelGroup.adultsOnly (+hotelthemes.adultsOnly).
- Sterne/Bewertung: „mind. 4 Sterne“ → stars_min:4 UND ui_filters.stars:["4"]; „8+“ → rating_min:8 UND ui_filters.ratings:["8"].
- Nur explizit genannte Daten/Gäste setzen; sonst leer lassen.
- Mehrdeutiges (z. B. „günstig“, „nahe“, „ruhig“) nicht raten → wortwörtlich in unsupported_criteria.
- Antworte nur mit dem JSON-Objekt (keine Erklärungen).
`

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			// Allow GET for /v1/evaluations and POST for /v1/parse
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ====== Input + Output types ======
type parseInput struct {
	Query    string `json:"query_de"`
	Provider string `json:"provider"` // "openai", "claude", "both"
}

type MultiParseResponse struct {
	OpenAI *ParseResponse `json:"openai,omitempty"`
	Claude *ParseResponse `json:"claude,omitempty"`
}

// ====== Parse handler ======
func parseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input parseInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Printf("[ERROR] invalid JSON body: %v", err)
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.Query) == "" {
		log.Printf("[WARN] empty query from client")
		http.Error(w, "query_de is required", http.StatusBadRequest)
		return
	}

	log.Printf("[INFO] Request: provider=%s query=%q", input.Provider, input.Query)

	_ = godotenv.Load()

	// Load system prompt (allow local overrides)
	systemPrompt := defaultSystemPrompt
	if b, err := os.ReadFile("prompt/system.txt"); err == nil {
		systemPrompt = string(b)
	}
	if b, err := os.ReadFile("prompt/examples.json"); err == nil {
		systemPrompt += "\n\nBeispiele (nur zur Steuerung, nicht ausgeben):\n" + string(b)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	results := MultiParseResponse{}
	requestStart := time.Now()

	run := func(cli LLMClient, provider string) (*ParseResponse, error) {
		start := time.Now()
		raw, err := cli.CompleteJSON(ctx, systemPrompt, input.Query)
		if err != nil {
			log.Printf("[ERROR] %s completion failed: %v", provider, err)
			return nil, err
		}
		jsonPart, err := extractJSONObject(raw)
		if err != nil {
			log.Printf("[ERROR] %s no JSON found: %s", provider, raw)
			return nil, fmt.Errorf("no JSON found in output: %s", raw)
		}
		var parsed ParseResponse
		dec := json.NewDecoder(strings.NewReader(jsonPart))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&parsed); err != nil {
			log.Printf("[ERROR] %s schema violation: %v", provider, err)
			return nil, err
		}
		if err := parsed.Validate(); err != nil {
			log.Printf("[ERROR] %s validation failed: %v", provider, err)
			return nil, err
		}
		log.Printf("[INFO] %s parsed successfully in %s", provider, time.Since(start))
		return &parsed, nil
	}

	switch strings.ToLower(strings.TrimSpace(input.Provider)) {
	case "claude":
		cli, err := NewClaudeClient()
		if err != nil {
			http.Error(w, "Claude client error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if res, err := run(cli, "Claude"); err == nil {
			results.Claude = res
		} else {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

	case "both":
		if cli, err := NewOpenAIClient(); err == nil {
			if res, err := run(cli, "OpenAI"); err == nil {
				results.OpenAI = res
			}
		}
		if cli, err := NewClaudeClient(); err == nil {
			if res, err := run(cli, "Claude"); err == nil {
				results.Claude = res
			}
		}
		if results.OpenAI == nil && results.Claude == nil {
			http.Error(w, "both calls failed", http.StatusBadGateway)
			return
		}

	default: // "openai" (or empty)
		cli, err := NewOpenAIClient()
		if err != nil {
			http.Error(w, "OpenAI client error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if res, err := run(cli, "OpenAI"); err == nil {
			results.OpenAI = res
		} else {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}

	// Persist the run for evaluations
	totalLatency := time.Since(requestStart).Milliseconds()
	StoreResult(input.Query, results, totalLatency)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

func main() {
	_ = godotenv.Load()
	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/parse", corsMiddleware(http.HandlerFunc(parseHandler)))
	mux.Handle("/v1/evaluations", corsMiddleware(http.HandlerFunc(evalHandler)))

	log.Println("Server on " + addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// extractJSONObject scans for the first balanced JSON object
func extractJSONObject(s string) (string, error) {
	start := -1
	depth := 0
	for i, r := range s {
		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if r == '}' {
			if depth > 0 {
				depth--
				if depth == 0 && start != -1 {
					return s[start : i+1], nil
				}
			}
		}
	}
	return "", errors.New("no balanced JSON object found")
}
