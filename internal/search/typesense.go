// internal/search/typesense.go
// Typesense search adapter for GEOLINK autocomplete queries.
package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/typesense/typesense-go/typesense"
	"github.com/typesense/typesense-go/typesense/api"

	"github.com/barathsuresh/geolink/internal/models"
)

const (
	defaultLimit = 10
	maxLimit     = 50
	defaultPage  = 1
)

// Search executes an autocomplete query against Typesense and returns
// a slice of SearchResult, the total number of matching documents, and any error.
func Search(
	ctx context.Context,
	client *typesense.Client,
	collection string,
	req models.SearchRequest,
) ([]models.SearchResult, int, error) {
	// ── Defaults & clamps ────────────────────────────────────────────────────
	limit := req.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	page := req.Page
	if page <= 0 {
		page = defaultPage
	}

	// ── Build filter_by ──────────────────────────────────────────────────────
	var filters []string
	if req.CountryCode != "" {
		filters = append(filters, fmt.Sprintf("country_code:=%s", req.CountryCode))
	}
	if req.FeatureCode != "" {
		filters = append(filters, fmt.Sprintf("feature_code:=%s", req.FeatureCode))
	}
	filterBy := strings.Join(filters, " && ")

	// ── Build search parameters ───────────────────────────────────────────────
	queryBy := "name,ascii_name,alternate_names"
	sortBy := "population:desc"
	perPage := limit

	params := &api.SearchCollectionParams{
		Q:       req.Query,
		QueryBy: queryBy,
		SortBy:  &sortBy,
		PerPage: &perPage,
		Page:    &page,
	}
	if filterBy != "" {
		params.FilterBy = &filterBy
	}

	// ── Execute ───────────────────────────────────────────────────────────────
	resp, err := client.Collection(collection).Documents().Search(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("typesense search: %w", err)
	}

	total := 0
	if resp.Found != nil {
		total = *resp.Found
	}

	// ── Map hits → SearchResult ───────────────────────────────────────────────
	results := make([]models.SearchResult, 0, len(*resp.Hits))
	for _, hit := range *resp.Hits {
		if hit.Document == nil {
			continue
		}
		doc := *hit.Document

		r := models.SearchResult{
			GeonameID:    getInt64(doc, "geoname_id"),
			Name:         getString(doc, "name"),
			CountryCode:  getString(doc, "country_code"),
			FeatureCode:  getString(doc, "feature_code"),
			FeatureLabel: models.GetFeatureLabel(getString(doc, "feature_code")),
			Population:   getInt64(doc, "population"),
			Latitude:     getFloat64(doc, "latitude"),
			Longitude:    getFloat64(doc, "longitude"),
		}

		// Normalise text_match score to [0, 1].
		// Typesense text_match scores are large integers; we cap at 1_000_000.
		if hit.TextMatch != nil {
			const maxScore = 1_000_000.0
			score := float64(*hit.TextMatch) / maxScore
			if score > 1.0 {
				score = 1.0
			}
			r.Score = score
		}

		results = append(results, r)
	}

	return results, total, nil
}

// ── Document field extractors ─────────────────────────────────────────────────

func getString(doc map[string]interface{}, key string) string {
	if v, ok := doc[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt64(doc map[string]interface{}, key string) int64 {
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

func getFloat64(doc map[string]interface{}, key string) float64 {
	if v, ok := doc[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}
