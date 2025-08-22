package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type StoredResult struct {
	Query    string             `json:"query"`
	Response MultiParseResponse `json:"response"`
	Latency  int64              `json:"latency_ms"`
	Time     time.Time          `json:"time"`
}

type GroundTruthItem struct {
	Query                    string          `json:"query"`
	Truth                    ParseResponse   `json:"truth"`
	Ambiguous                bool            `json:"ambiguous,omitempty"`
	AcceptableInterpretation []ParseResponse `json:"acceptable_interpretations,omitempty"`
}

const resultsFile = "data/results.json"
const groundFile = "data/groundtruth.json"

// Append new result in JSON "db"
func StoreResult(query string, resp MultiParseResponse, latency int64) {
	var results []StoredResult
	_ = os.MkdirAll("data", 0755)
	if b, err := os.ReadFile(resultsFile); err == nil {
		_ = json.Unmarshal(b, &results)
	}
	results = append(results, StoredResult{Query: query, Response: resp, Latency: latency, Time: time.Now()})
	b, _ := json.MarshalIndent(results, "", "  ")
	_ = os.WriteFile(resultsFile, b, 0644)
}

// ===== Evaluation types =====

type SlotStats struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	FN int `json:"fn"`
}

type ProviderMetrics struct {
	SlotPrecision         float64 `json:"slot_precision"` // micro across all slots
	SlotRecall            float64 `json:"slot_recall"`
	F1                    float64 `json:"f1"`          // harmonic of micro P/R
	ExactMatch            float64 `json:"exact_match"` // mean over queries
	Jaccard               float64 `json:"jaccard"`     // mean over queries
	AvgLatencyMS          float64 `json:"avg_latency_ms"`
	Count                 int     `json:"count"` // queries with ground truth
	AmbiguityHandlingRate float64 `json:"ambiguity_handling_rate"`
	PerSlot               map[string]struct {
		Precision float64 `json:"precision"`
		Recall    float64 `json:"recall"`
		F1        float64 `json:"f1"`
		TP        int     `json:"tp"`
		FP        int     `json:"fp"`
		FN        int     `json:"fn"`
	} `json:"per_slot"`
}

type QueryScores struct {
	ExactMatch bool    `json:"exact_match"`
	Jaccard    float64 `json:"jaccard"`
	F1         float64 `json:"f1"`
	LatencyMS  int64   `json:"latency_ms"`
}

type PerQueryCompare struct {
	Query     string       `json:"query"`
	OpenAI    *QueryScores `json:"openai,omitempty"`
	Claude    *QueryScores `json:"claude,omitempty"`
	Ambiguous bool         `json:"ambiguous"`
	Accepted  bool         `json:"accepted"` // if any provider matched an acceptable interpretation
	Time      time.Time    `json:"time"`
}

type EvalResponse struct {
	OpenAI       *ProviderMetrics  `json:"openai,omitempty"`
	Claude       *ProviderMetrics  `json:"claude,omitempty"`
	PerQueryDiff []PerQueryCompare `json:"per_query,omitempty"` // when ?per_query=1
}

// ===== HTTP handler =====

func evalHandler(w http.ResponseWriter, r *http.Request) {
	// Raw mode: return stored runs exactly as logged
	if r.URL.Query().Get("raw") == "1" {
		b, err := os.ReadFile(resultsFile)
		if err != nil {
			http.Error(w, "no results yet", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
		return
	}

	// Load results
	var results []StoredResult
	if b, err := os.ReadFile(resultsFile); err == nil {
		_ = json.Unmarshal(b, &results)
	}

	// Load ground truth
	var gtItems []GroundTruthItem
	if b, err := os.ReadFile(groundFile); err == nil {
		_ = json.Unmarshal(b, &gtItems)
	}

	// Map query -> ground truth item
	gtMap := map[string]GroundTruthItem{}
	for _, g := range gtItems {
		gtMap[g.Query] = g
	}

	openAcc := newAcc()
	claudeAcc := newAcc()

	wantPerQuery := r.URL.Query().Get("per_query") == "1"
	var perQuery []PerQueryCompare

	for _, run := range results {
		gtItem, ok := gtMap[run.Query]
		if !ok {
			continue // skip runs with no ground truth
		}
		gt := gtItem.Truth

		if run.Response.OpenAI != nil {
			s := scoreAgainstGT(*run.Response.OpenAI, gt)
			openAcc.add(s, run.Latency)
			openAcc.addSlots(*run.Response.OpenAI, gt)
			if gtItem.Ambiguous {
				if matchesAnyAcceptable(*run.Response.OpenAI, gtItem.AcceptableInterpretation) {
					openAcc.ambAccepted++
				}
				openAcc.ambTotal++
			}
			if wantPerQuery {
				perQuery = upsertPerQuery(perQuery, run, "openai", s, gtItem.Ambiguous, gtItem.AcceptableInterpretation)
			}
		}
		if run.Response.Claude != nil {
			s := scoreAgainstGT(*run.Response.Claude, gt)
			claudeAcc.add(s, run.Latency)
			claudeAcc.addSlots(*run.Response.Claude, gt)
			if gtItem.Ambiguous {
				if matchesAnyAcceptable(*run.Response.Claude, gtItem.AcceptableInterpretation) {
					claudeAcc.ambAccepted++
				}
				claudeAcc.ambTotal++
			}
			if wantPerQuery {
				perQuery = upsertPerQuery(perQuery, run, "claude", s, gtItem.Ambiguous, gtItem.AcceptableInterpretation)
			}
		}
	}

	resp := EvalResponse{}
	if openAcc.n > 0 {
		m := openAcc.metrics()
		resp.OpenAI = &m
	}
	if claudeAcc.n > 0 {
		m := claudeAcc.metrics()
		resp.Claude = &m
	}
	if wantPerQuery {
		sort.Slice(perQuery, func(i, j int) bool { return perQuery[i].Time.Before(perQuery[j].Time) })
		resp.PerQueryDiff = perQuery
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// ===== Internals for metrics =====

type acc struct {
	// micro totals
	tp int
	fp int
	fn int

	// per-query aggregates
	sumExact int
	sumJac   float64
	sumF1    float64
	sumLat   float64
	n        int

	// ambiguity
	ambAccepted int
	ambTotal    int

	// per-slot stats
	slot map[string]*SlotStats
}

func newAcc() *acc { return &acc{slot: map[string]*SlotStats{}} }

func (a *acc) add(q QueryScores, latency int64) {
	if q.ExactMatch {
		a.sumExact++
	}
	a.sumJac += q.Jaccard
	a.sumF1 += q.F1
	a.sumLat += float64(latency)
	a.n++
}

func (a *acc) addSlots(pred ParseResponse, gt ParseResponse) {
	pSet := flattenWithSlots(pred)
	gSet := flattenWithSlots(gt)

	for k := range pSet {
		if gSet[k] {
			a.tp++
			slot := slotNameOf(k)
			incSlot(a.slot, slot, 1, 0, 0)
		} else {
			a.fp++
			slot := slotNameOf(k)
			incSlot(a.slot, slot, 0, 1, 0)
		}
	}
	for k := range gSet {
		if !pSet[k] {
			a.fn++
			slot := slotNameOf(k)
			incSlot(a.slot, slot, 0, 0, 1)
		}
	}
}

func (a *acc) metrics() ProviderMetrics {
	prec := safeDiv(a.tp, a.tp+a.fp)
	rec := safeDiv(a.tp, a.tp+a.fn)
	f1 := harm(prec, rec)

	avgExact := 0.0
	avgJac := 0.0

	avgLat := 0.0
	if a.n > 0 {
		avgExact = float64(a.sumExact) / float64(a.n)
		avgJac = a.sumJac / float64(a.n)

		avgLat = a.sumLat / float64(a.n)
	}

	perSlot := map[string]struct {
		Precision float64 `json:"precision"`
		Recall    float64 `json:"recall"`
		F1        float64 `json:"f1"`
		TP        int     `json:"tp"`
		FP        int     `json:"fp"`
		FN        int     `json:"fn"`
	}{}
	for slot, s := range a.slot {
		p := safeDiv(s.TP, s.TP+s.FP)
		r := safeDiv(s.TP, s.TP+s.FN)
		perSlot[slot] = struct {
			Precision float64 `json:"precision"`
			Recall    float64 `json:"recall"`
			F1        float64 `json:"f1"`
			TP        int     `json:"tp"`
			FP        int     `json:"fp"`
			FN        int     `json:"fn"`
		}{
			Precision: round2(p),
			Recall:    round2(r),
			F1:        round2(harm(p, r)),
			TP:        s.TP, FP: s.FP, FN: s.FN,
		}
	}

	ambRate := 0.0
	if a.ambTotal > 0 {
		ambRate = float64(a.ambAccepted) / float64(a.ambTotal)
	}

	return ProviderMetrics{
		SlotPrecision:         round2(prec),
		SlotRecall:            round2(rec),
		F1:                    round2(f1),
		ExactMatch:            round2(avgExact),
		Jaccard:               round2(avgJac),
		AvgLatencyMS:          round2(avgLat),
		Count:                 a.n,
		AmbiguityHandlingRate: round2(ambRate),
		PerSlot:               perSlot,
	}
}

func incSlot(m map[string]*SlotStats, slot string, tp, fp, fn int) {
	s := m[slot]
	if s == nil {
		s = &SlotStats{}
		m[slot] = s
	}
	s.TP += tp
	s.FP += fp
	s.FN += fn
}

func scoreAgainstGT(pred ParseResponse, gt ParseResponse) QueryScores {
	pSet := flatten(pred)
	gSet := flatten(gt)

	inter := 0
	for k := range pSet {
		if gSet[k] {
			inter++
		}
	}
	union := len(pSet) + len(gSet) - inter

	prec := safeDiv(inter, len(pSet))
	rec := safeDiv(inter, len(gSet))
	f1 := harm(prec, rec)

	jac := 0.0
	if union > 0 {
		jac = float64(inter) / float64(union)
	}
	return QueryScores{
		ExactMatch: setsEqual(pSet, gSet),
		Jaccard:    round2(jac),
		F1:         round2(f1),
		LatencyMS:  0, // filled by caller if desired
	}
}

func upsertPerQuery(list []PerQueryCompare, run StoredResult, provider string, s QueryScores, ambiguous bool, acceptable []ParseResponse) []PerQueryCompare {
	idx := -1
	for i := range list {
		if list[i].Query == run.Query && list[i].Time.Equal(run.Time) {
			idx = i
			break
		}
	}
	if idx == -1 {
		list = append(list, PerQueryCompare{
			Query:     run.Query,
			Ambiguous: ambiguous,
			Time:      run.Time,
		})
		idx = len(list) - 1
	}
	q := list[idx]
	switch provider {
	case "openai":
		s.LatencyMS = run.Latency
		q.OpenAI = &s
	case "claude":
		s.LatencyMS = run.Latency
		q.Claude = &s
	}
	// accepted if any provider matched an acceptable interpretation
	if ambiguous {
		accepted := false
		if run.Response.OpenAI != nil && matchesAnyAcceptable(*run.Response.OpenAI, acceptable) {
			accepted = true
		}
		if run.Response.Claude != nil && matchesAnyAcceptable(*run.Response.Claude, acceptable) {
			accepted = true
		}
		q.Accepted = accepted
	}
	list[idx] = q
	return list
}

// ===== Sets / flatten helpers =====

func matchesAnyAcceptable(pred ParseResponse, accepts []ParseResponse) bool {
	if len(accepts) == 0 {
		return false
	}
	pSet := flatten(pred)
	for _, a := range accepts {
		if setsEqual(pSet, flatten(a)) {
			return true
		}
	}
	return false
}

func flatten(p ParseResponse) map[string]bool {
	s := map[string]bool{}

	if p.Location != "" {
		s["location="+p.Location] = true
	}
	if p.Dates.Checkin != "" {
		s["dates.checkin="+p.Dates.Checkin] = true
	}
	if p.Dates.Checkout != "" {
		s["dates.checkout="+p.Dates.Checkout] = true
	}
	if p.Guests.Adults != 0 {
		s[fmt.Sprintf("guests.adults=%d", p.Guests.Adults)] = true
	}
	if p.Guests.Children != 0 {
		s[fmt.Sprintf("guests.children=%d", p.Guests.Children)] = true
	}
	if p.PriceMaxEUR != 0 {
		s[fmt.Sprintf("price_max_eur=%.0f", p.PriceMaxEUR)] = true
	}
	if p.StarsMin != 0 {
		s[fmt.Sprintf("stars_min=%d", p.StarsMin)] = true
	}
	if p.RatingMin != 0 {
		s[fmt.Sprintf("rating_min=%.1f", p.RatingMin)] = true
	}
	s[fmt.Sprintf("family_friendly=%t", p.FamilyFriendly)] = true

	addSlice := func(prefix string, arr []string) {
		for _, v := range arr {
			v = strings.TrimSpace(v)
			if v != "" {
				s[prefix+"="+v] = true
			}
		}
	}
	addSlice("ui.meals", p.UiFilters.Meals)
	addSlice("ui.ratings", p.UiFilters.Ratings)
	addSlice("ui.hotelTypes", p.UiFilters.HotelTypes)
	addSlice("ui.hotelfacilities", p.UiFilters.Hotelfacilities)
	addSlice("ui.poolbeach", p.UiFilters.Poolbeach)
	addSlice("ui.distanceBeach", p.UiFilters.DistanceBeach)
	addSlice("ui.travelGroup", p.UiFilters.TravelGroup)
	addSlice("ui.stars", p.UiFilters.Stars)
	addSlice("ui.wellness", p.UiFilters.Wellness)
	addSlice("ui.reference_distance_max", p.UiFilters.ReferenceDistance)
	addSlice("ui.flex", p.UiFilters.Flex)
	addSlice("ui.children", p.UiFilters.Children)
	addSlice("ui.parking", p.UiFilters.Parking)
	addSlice("ui.freetime", p.UiFilters.Freetime)
	addSlice("ui.certifications", p.UiFilters.Certifications)
	addSlice("ui.hotelthemes", p.UiFilters.Hotelthemes)
	addSlice("ui.hotelBrand", p.UiFilters.HotelBrand)
	addSlice("ui.hotelinformation", p.UiFilters.Hotelinformation)

	for _, v := range p.UnsupportedCriteria {
		v = strings.TrimSpace(v)
		if v != "" {
			s["unsupported="+v] = true
		}
	}
	return s
}

func flattenWithSlots(p ParseResponse) map[string]bool {
	return flatten(p) // keys retain slot path (e.g., "ui.meals=value")
}

func slotNameOf(k string) string {
	if i := strings.IndexByte(k, '='); i > 0 {
		return k[:i]
	}
	return k
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func safeDiv(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func harm(p, r float64) float64 {
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}
