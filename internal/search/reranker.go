// internal/search/reranker.go
// Personalization re-ranker: boosts Typesense results using the IP profile.
package search

import (
	"sort"

	"github.com/barathsuresh/geolink/internal/models"
	"github.com/barathsuresh/geolink/pkg/geoip"
)

// Rerank applies personalization weights to results using the caller's IP profile.
// If profile is nil, results are returned unchanged.
//
// Score formula:
//
//	finalScore = 1.0                             (normalised base — equal for all)
//	           + 0.40 * countryWeight            (same country as past searches)
//	           + 0.20 * continentWeight          (same continent)
//	           + 0.15 * featureWeight            (same feature type, e.g. Airport, City)
//	           + 0.25 * recentMatch              (query appeared in recent searches)
//	           + 0.0001 * populationNorm         (tiebreaker within same country group)
//
// NOTE: baseScore is deliberately set to 1.0 for all results so that the
// profile signal — not Typesense's population-weighted relevance score —
// controls ordering. Without this, a popular city in a non-preferred country
// (e.g. Tempe, Arizona pop=175K) would always outrank any result from the
// user's actual preferred country just by having a higher Typesense score.
func Rerank(results []models.SearchResult, profile *models.IPProfile) []models.SearchResult {
	if profile == nil {
		return results
	}

	// Pre-compute max values to normalise each dimension to [0, 1].
	maxCountry := maxVal(profile.Countries)
	maxContinent := maxVal(profile.Continents)
	maxFeature := maxVal(profile.FeatureCodes)

	// Pre-compute max population for tiebreaker normalisation.
	maxPop := int64(1)
	for _, r := range results {
		if r.Population > maxPop {
			maxPop = r.Population
		}
	}

	// Build a set of recent queries for O(1) lookup.
	recentSet := make(map[string]struct{}, len(profile.Recent))
	for _, q := range profile.Recent {
		recentSet[q] = struct{}{}
	}

	for i := range results {
		r := &results[i]

		// Normalise — all results start equal so profile controls order.
		baseScore := 1.0

		// Country signal.
		countryWeight := 0.0
		if maxCountry > 0 {
			countryWeight = clamp01(float64(profile.Countries[r.CountryCode]) / float64(maxCountry))
		}

		// Continent signal.
		continent := geoip.GetContinent(r.CountryCode)
		continentWeight := 0.0
		if maxContinent > 0 {
			continentWeight = clamp01(float64(profile.Continents[continent]) / float64(maxContinent))
		}

		// Feature-code signal.
		featureWeight := 0.0
		if maxFeature > 0 {
			featureWeight = clamp01(float64(profile.FeatureCodes[r.FeatureCode]) / float64(maxFeature))
		}

		// Recent-query signal.
		recentMatch := 0.0
		if _, ok := recentSet[r.Name]; ok {
			recentMatch = 1.0
		}

		// Population tiebreaker (tiny weight — only matters when profile scores are equal).
		popNorm := clamp01(float64(r.Population) / float64(maxPop))

		finalScore := baseScore +
			0.40*countryWeight +
			0.20*continentWeight +
			0.15*featureWeight +
			0.25*recentMatch +
			0.0001*popNorm

		r.Boosted = countryWeight > 0 || continentWeight > 0.5 || recentMatch > 0
		r.Score = finalScore
	}

	// Sort descending by final score.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// maxVal returns the maximum integer value in a map, or 0 if the map is empty.
func maxVal(m map[string]int) int {
	max := 0
	for _, v := range m {
		if v > max {
			max = v
		}
	}
	return max
}

// clamp01 clamps a float64 to the [0, 1] interval.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
