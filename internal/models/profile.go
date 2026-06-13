// internal/models/profile.go
// IP-keyed personalization profile model.
// Stored in Redis as a JSON blob at key "profile:<ip>".
package models

// IPProfile aggregates historical search signals for a single IP address.
// It is used by the re-ranker to personalise autocomplete result ordering.
type IPProfile struct {
	IP           string         `json:"ip"`
	Countries    map[string]int `json:"countries"`     // country_code → search count
	Continents   map[string]int `json:"continents"`    // continent name → search count
	FeatureCodes map[string]int `json:"feature_codes"` // feature_code → search count
	Recent       []string       `json:"recent_searches"` // ring buffer, capped at RecentSearchesLimit
}
