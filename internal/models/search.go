// internal/models/search.go
// Request/response types and event types for the GEOLINK search API.
package models

// ─── API Request/Response ─────────────────────────────────────────────────────

// SearchRequest represents a validated autocomplete query.
// Bound from query-string parameters via Gin's ShouldBindQuery.
type SearchRequest struct {
	Query        string `form:"q"            binding:"required,min=1,max=100"`
	IP           string `form:"ip"`
	Personalized bool   `form:"personalized"`
	Limit        int    `form:"limit"`
	Page         int    `form:"page"`
	CountryCode  string `form:"country_code"`
	FeatureCode  string `form:"feature_code"`
}

// SearchResult is a single autocomplete suggestion returned to the client.
type SearchResult struct {
	GeonameID   int64   `json:"geoname_id"`
	Name        string  `json:"name"`
	CountryCode string  `json:"country_code"`
	CountryName string  `json:"country_name"`
	FeatureCode string  `json:"feature_code"`
	FeatureLabel string `json:"feature_label"`
	Population  int64   `json:"population"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Score       float64 `json:"score"`   // re-ranker score (higher = better)
	Boosted     bool    `json:"boosted"` // true if personalization boosted this result
}

// SearchResponse is the top-level API response envelope.
type SearchResponse struct {
	Query        string         `json:"query"`
	Results      []SearchResult `json:"results"`
	Total        int            `json:"total"`
	Page         int            `json:"page"`
	Limit        int            `json:"limit"`
	Personalized bool           `json:"personalized"`
	TimeTakenMs  float64        `json:"time_taken_ms"`
}

// ─── Kafka Event ──────────────────────────────────────────────────────────────

// SearchEvent is published to the "search.queries" Kafka topic after each
// autocomplete request. Consumed by the personalizer worker.
type SearchEvent struct {
	IP          string `json:"ip"`
	Query       string `json:"query"`
	CountryCode string `json:"top_result_country"`
	FeatureCode string `json:"top_result_feature_code"`
	Continent   string `json:"continent"`
	Timestamp   int64  `json:"timestamp"` // Unix millis
}

// ─── Admin Toggle ─────────────────────────────────────────────────────────────

// ToggleRequest is the body of PUT /toggle/global and PUT /toggle/ip.
// Note: binding:"required" on a bool rejects false (zero value), so we omit it.
type ToggleRequest struct {
	Enabled bool   `json:"enabled"`
	IP      string `json:"ip"` // required only for /toggle/ip
}

// ToggleResponse confirms the new toggle state to the admin caller.
type ToggleResponse struct {
	Scope   string `json:"scope"`             // "global" or "ip"
	IP      string `json:"ip,omitempty"`      // set when Scope == "ip"
	Enabled bool   `json:"enabled"`
}
