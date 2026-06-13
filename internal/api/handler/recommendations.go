// internal/api/handler/recommendations.go
// GET /api/v1/recommendations — returns two parallel recommendation sections:
//  1. "Based on your searches"  — cities from the user's most-searched countries
//  2. "Near you"                — cities near the user's physical location (geo-IP)
// Both sections are fetched concurrently and returned together.
package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/typesense/typesense-go/typesense"
	tsapi "github.com/typesense/typesense-go/typesense/api"

	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/geo"
	"github.com/barathsuresh/geolink/internal/models"
	"github.com/barathsuresh/geolink/internal/personalization"
)

// RecommendationsDeps wires handler dependencies.
type RecommendationsDeps struct {
	TSClient   *typesense.Client
	RDB        *redis.Client
	Cfg        *config.Config
	Collection string
}

// RecommendationItem is a single recommended place.
type RecommendationItem struct {
	models.SearchResult
	Reason string `json:"reason"` // "history" | "nearby" | "global_popular"
}

// RecommendationSection is one labelled group of recommendations.
type RecommendationSection struct {
	Source  string               `json:"source"`  // "history" | "nearby" | "global_popular"
	Label   string               `json:"label"`   // shown in the UI
	Icon    string               `json:"icon"`    // emoji icon
	Items   []RecommendationItem `json:"items"`
}

// RecommendationsResponse returns all sections in one call.
type RecommendationsResponse struct {
	IP          string                  `json:"ip"`
	Detected    *geoInfo                `json:"detected_location,omitempty"`
	Sections    []RecommendationSection `json:"sections"`
	TimeTakenMs float64                 `json:"time_taken_ms"`
}

type geoInfo struct {
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// Recommendations handles GET /api/v1/recommendations.
// It fetches history-based and geo-IP-based sections concurrently.
func Recommendations(deps RecommendationsDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		t0 := time.Now()

		// ── Resolve IP ────────────────────────────────────────────────────────
		ip, _ := c.Get("client_ip")
		ipStr, _ := ip.(string)
		if override := c.Query("ip"); override != "" {
			ipStr = override
		}

		limit := 8
		if n, err := strconv.Atoi(c.DefaultQuery("limit", "8")); err == nil {
			limit = n
		}
		if limit <= 0 || limit > 25 {
			limit = 8
		}

		ctx := c.Request.Context()

		// ── Fetch both sections concurrently ──────────────────────────────────
		var (
			historySection RecommendationSection
			nearbySection  RecommendationSection
			location       *geo.Location
			wg             sync.WaitGroup
		)

		// Section 1: history-based (Redis profile)
		// Only activate if the user has a meaningful signal (≥5 searches in any country).
		// This prevents a single accidental search from triggering the section.
		wg.Add(1)
		go func() {
			defer wg.Done()
			profile, _ := personalization.GetProfile(ctx, deps.RDB, ipStr)
			if profile != nil && profileHasSignal(profile, 5) {
				items := profileRecommendations(ctx, deps, profile, limit)
				if len(items) > 0 {
					historySection = RecommendationSection{
						Source: "history",
						Label:  "Based on your searches",
						Icon:   "✦",
						Items:  items,
					}
				}
			}
		}()

		// Section 2: geo-IP based (physical location)
		wg.Add(1)
		go func() {
			defer wg.Done()
			loc, _ := geo.LookupIP(ctx, ipStr)
			if loc == nil {
				return
			}
			location = loc
			items := nearbyRecommendations(ctx, deps, loc, limit)
			if len(items) > 0 {
				label := "Near you"
				if loc.City != "" {
					label = "Near " + loc.City
				} else if loc.Country != "" {
					label = "Near " + loc.Country
				}
				nearbySection = RecommendationSection{
					Source: "nearby",
					Label:  label,
					Icon:   "📍",
					Items:  items,
				}
			}
		}()

		wg.Wait()

		// ── Assemble sections ─────────────────────────────────────────────────
		var sections []RecommendationSection

		if len(historySection.Items) > 0 {
			sections = append(sections, historySection)
		}
		if len(nearbySection.Items) > 0 {
			sections = append(sections, nearbySection)
		}

		// If both empty, fall back to global popular.
		if len(sections) == 0 {
			items := globalRecommendations(ctx, deps, limit)
			if len(items) > 0 {
				sections = append(sections, RecommendationSection{
					Source: "global_popular",
					Label:  "Popular places",
					Icon:   "🌍",
					Items:  items,
				})
			}
		}

		var detected *geoInfo
		if location != nil {
			detected = &geoInfo{
				Country:     location.Country,
				CountryCode: location.CountryCode,
				City:        location.City,
				Lat:         location.Lat,
				Lng:         location.Lng,
			}
		}

		c.JSON(http.StatusOK, RecommendationsResponse{
			IP:          ipStr,
			Detected:    detected,
			Sections:    sections,
			TimeTakenMs: float64(time.Since(t0).Milliseconds()),
		})
	}
}

// profileRecommendations fetches top cities from the user's most-searched countries.
func profileRecommendations(ctx context.Context, deps RecommendationsDeps, profile *models.IPProfile, limit int) []RecommendationItem {
	type kv struct {
		country string
		count   int
	}
	sorted := make([]kv, 0, len(profile.Countries))
	for cc, cnt := range profile.Countries {
		sorted = append(sorted, kv{cc, cnt})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	maxCountries := 3
	if len(sorted) < maxCountries {
		maxCountries = len(sorted)
	}

	seen := make(map[int64]bool)
	var items []RecommendationItem
	perCountry := (limit / maxCountries) + 2

	for i := 0; i < maxCountries && len(items) < limit; i++ {
		hits := searchByCountry(ctx, deps, sorted[i].country, perCountry)
		for _, r := range hits {
			if seen[r.GeonameID] || len(items) >= limit {
				continue
			}
			seen[r.GeonameID] = true
			items = append(items, RecommendationItem{SearchResult: r, Reason: "history"})
		}
	}
	return items
}

// nearbyRecommendations finds notable cities in the same country as the IP,
// within a ±5° bounding box. Country filter is applied first to prevent
// cross-border results (e.g. Mexican cities appearing for Tempe, AZ).
func nearbyRecommendations(ctx context.Context, deps RecommendationsDeps, loc *geo.Location, limit int) []RecommendationItem {
	const delta = 5.0
	sortBy := "population:desc"

	// Build filter: bounding box + same country (prevents cross-border leakage).
	filterBy := fmt.Sprintf(
		"latitude:>=%f && latitude:<=%f && longitude:>=%f && longitude:<=%f && feature_class:=P",
		loc.Lat-delta, loc.Lat+delta, loc.Lng-delta, loc.Lng+delta,
	)
	if loc.CountryCode != "" {
		filterBy += fmt.Sprintf(" && country_code:=%s", loc.CountryCode)
	}

	q := "*"
	perPage := limit
	params := &tsapi.SearchCollectionParams{
		Q: q, QueryBy: "name", SortBy: &sortBy, FilterBy: &filterBy, PerPage: &perPage,
	}
	resp, err := deps.TSClient.Collection(deps.Collection).Documents().Search(ctx, params)
	if err == nil && resp.Hits != nil && len(*resp.Hits) > 0 {
		return toItems(hitsToResults(*resp.Hits), "nearby")
	}
	// Widen to whole country if bounding box + country returns nothing.
	if loc.CountryCode != "" {
		return toItems(searchByCountry(ctx, deps, loc.CountryCode, limit), "nearby")
	}
	return nil
}

// profileHasSignal returns true if any country in the profile has at least minCount searches.
func profileHasSignal(profile *models.IPProfile, minCount int) bool {
	for _, cnt := range profile.Countries {
		if cnt >= minCount {
			return true
		}
	}
	return false
}

// searchByCountry returns top cities for a country sorted by population.
func searchByCountry(ctx context.Context, deps RecommendationsDeps, countryCode string, limit int) []models.SearchResult {
	sortBy := "population:desc"
	filterBy := fmt.Sprintf("country_code:=%s && feature_class:=P", countryCode)
	q := "*"
	perPage := limit
	params := &tsapi.SearchCollectionParams{
		Q: q, QueryBy: "name", SortBy: &sortBy, FilterBy: &filterBy, PerPage: &perPage,
	}
	resp, err := deps.TSClient.Collection(deps.Collection).Documents().Search(ctx, params)
	if err != nil || resp.Hits == nil {
		return nil
	}
	return hitsToResults(*resp.Hits)
}

// globalRecommendations returns the world's most populous cities.
func globalRecommendations(ctx context.Context, deps RecommendationsDeps, limit int) []RecommendationItem {
	sortBy := "population:desc"
	filterBy := "feature_class:=P && population:>500000"
	q := "*"
	perPage := limit
	params := &tsapi.SearchCollectionParams{
		Q: q, QueryBy: "name", SortBy: &sortBy, FilterBy: &filterBy, PerPage: &perPage,
	}
	resp, err := deps.TSClient.Collection(deps.Collection).Documents().Search(ctx, params)
	if err != nil || resp.Hits == nil {
		return nil
	}
	return toItems(hitsToResults(*resp.Hits), "global_popular")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func toItems(results []models.SearchResult, reason string) []RecommendationItem {
	items := make([]RecommendationItem, 0, len(results))
	for _, r := range results {
		items = append(items, RecommendationItem{SearchResult: r, Reason: reason})
	}
	return items
}

func hitsToResults(hits []tsapi.SearchResultHit) []models.SearchResult {
	results := make([]models.SearchResult, 0, len(hits))
	for _, hit := range hits {
		if hit.Document == nil {
			continue
		}
		doc := *hit.Document
		results = append(results, models.SearchResult{
			GeonameID:    getInt64Field(doc, "geoname_id"),
			Name:         getStringField(doc, "name"),
			CountryCode:  getStringField(doc, "country_code"),
			FeatureCode:  getStringField(doc, "feature_code"),
			FeatureLabel: models.GetFeatureLabel(getStringField(doc, "feature_code")),
			Population:   getInt64Field(doc, "population"),
			Latitude:     getFloat64Field(doc, "latitude"),
			Longitude:    getFloat64Field(doc, "longitude"),
			Score:        1.0,
		})
	}
	return results
}

func getStringField(doc map[string]interface{}, key string) string {
	if v, ok := doc[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt64Field(doc map[string]interface{}, key string) int64 {
	if v, ok := doc[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

func getFloat64Field(doc map[string]interface{}, key string) float64 {
	if v, ok := doc[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}
