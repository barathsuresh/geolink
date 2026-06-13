// internal/api/handler/search.go
// GET /api/v1/search — autocomplete handler.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/typesense/typesense-go/typesense"

	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/kafka"
	"github.com/barathsuresh/geolink/internal/models"
	"github.com/barathsuresh/geolink/internal/personalization"
	"github.com/barathsuresh/geolink/internal/search"
	"github.com/barathsuresh/geolink/pkg/geoip"
)

// SearchDeps bundles the dependencies injected into the search handler.
type SearchDeps struct {
	TSClient   *typesense.Client
	RDB        *redis.Client
	Producer   *kafka.Producer
	Cfg        *config.Config
}

// Search handles GET /api/v1/search.
func Search(deps SearchDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── Bind & validate query params ──────────────────────────────────────
		var req models.SearchRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
				"code":  "INVALID_REQUEST",
			})
			return
		}

		// ── Resolve client IP ─────────────────────────────────────────────────
		clientIP, _ := c.Get("client_ip")
		ip, _ := clientIP.(string)
		if req.IP != "" {
			ip = req.IP // allow explicit override in query string
		}

		// ── Enforce limit bounds ──────────────────────────────────────────────
		if req.Limit <= 0 {
			req.Limit = 10
		}
		if req.Limit > 50 {
			req.Limit = 50
		}
		if req.Page <= 0 {
			req.Page = 1
		}

		start := time.Now()

		// ── Cache lookup (non-personalized requests only) ─────────────────────
		// Personalized results are per-IP so must never be served from a shared cache.
		var (
			results []models.SearchResult
			total   int
			cacheHit bool
		)

		cacheKey := searchCacheKey(req)
		if !req.Personalized && deps.Cfg.SearchCacheTTLSeconds > 0 {
			if cached, err := deps.RDB.Get(c.Request.Context(), cacheKey).Bytes(); err == nil {
				var cr cachedSearchResult
				if json.Unmarshal(cached, &cr) == nil {
					results, total, cacheHit = cr.Results, cr.Total, true
				}
			}
		}

		// ── Typesense search (cache miss) ─────────────────────────────────────
		if !cacheHit {
			var err error
			results, total, err = search.Search(c.Request.Context(), deps.TSClient, deps.Cfg.TypesenseCollection, req)
			if err != nil {
				slog.Error("typesense search failed", "err", err, "query", req.Query)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "search failed",
					"code":  "SEARCH_ERROR",
				})
				return
			}

			// Store in cache for non-personalized requests.
			if !req.Personalized && deps.Cfg.SearchCacheTTLSeconds > 0 {
				if b, err := json.Marshal(cachedSearchResult{Results: results, Total: total}); err == nil {
					ttl := time.Duration(deps.Cfg.SearchCacheTTLSeconds) * time.Second
					deps.RDB.Set(c.Request.Context(), cacheKey, b, ttl)
				}
			}
		}

		// ── Personalization + Country-Preferred Search ────────────────────────
		personalized := false
		if req.Personalized != false && personalization.IsPersonalizationEnabled(c.Request.Context(), deps.RDB, ip) {
			profile, err := personalization.GetProfile(c.Request.Context(), deps.RDB, ip)
			if err != nil {
				slog.Warn("get profile failed", "ip", ip, "err", err)
			} else if profile != nil {
				// Country-preferred search: if the user has a dominant country
				// (>30% of their searches), try searching within that country first.
				// This surfaces local results for queries like "Tempe" that have no
				// match in the user's home country via re-ranking alone.
				topCountry := dominantCountry(profile, 0.30)
				if topCountry != "" && req.CountryCode == "" {
					countryReq := req
					countryReq.CountryCode = topCountry
					countryReq.Limit = req.Limit / 2
					if countryReq.Limit < 3 {
						countryReq.Limit = 3
					}
					localResults, _, localErr := search.Search(c.Request.Context(), deps.TSClient, deps.Cfg.TypesenseCollection, countryReq)
					if localErr == nil && len(localResults) > 0 {
						// Merge: local results first, then global (de-duped).
						seen := make(map[int64]bool)
						merged := make([]models.SearchResult, 0, len(results))
						for _, r := range localResults {
							seen[r.GeonameID] = true
							merged = append(merged, r)
						}
						for _, r := range results {
							if !seen[r.GeonameID] {
								merged = append(merged, r)
							}
						}
						if len(merged) > req.Limit {
							merged = merged[:req.Limit]
						}
						results = merged
					}
				}
				results = search.Rerank(results, profile)
				personalized = true
			}
		}

		// ── Build response ────────────────────────────────────────────────────
		resp := models.SearchResponse{
			Query:        req.Query,
			Results:      results,
			Total:        total,
			Page:         req.Page,
			Limit:        req.Limit,
			Personalized: personalized,
			TimeTakenMs:  float64(time.Since(start).Milliseconds()),
		}
		c.JSON(http.StatusOK, resp)

	}
}

// RecordSearchEvent handles POST /api/v1/events/search.
// Called explicitly by the frontend when a user clicks/selects a result.
func RecordSearchEvent(deps SearchDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP, _ := c.Get("client_ip")
		ip, _ := clientIP.(string)

		var req struct {
			Query       string `json:"query" binding:"required"`
			CountryCode string `json:"country_code" binding:"required"`
			FeatureCode string `json:"feature_code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}

		continent := geoip.GetContinent(req.CountryCode)

		event := models.SearchEvent{
			IP:          ip,
			Query:       req.Query,
			CountryCode: req.CountryCode,
			FeatureCode: req.FeatureCode,
			Continent:   continent,
			Timestamp:   time.Now().Unix(),
		}
		data, err := json.Marshal(event)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode event"})
			return
		}

		produceCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := deps.Producer.Produce(produceCtx, ip, data); err != nil {
			slog.Warn("kafka produce search event failed", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record event"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "recorded"})
	}
}

// cachedSearchResult is the JSON envelope stored in Redis for search caching.
type cachedSearchResult struct {
	Results []models.SearchResult `json:"results"`
	Total   int                   `json:"total"`
}

// searchCacheKey builds a deterministic Redis key from the non-personalized
// search dimensions. IP and personalization flag are intentionally excluded.
func searchCacheKey(req models.SearchRequest) string {
	return fmt.Sprintf("search:%s:%d:%d:%s:%s",
		req.Query, req.Page, req.Limit, req.CountryCode, req.FeatureCode)
}

// dominantCountry returns the country that makes up more than `threshold` fraction
// of the user's total country searches. Returns "" if no country is that dominant.
func dominantCountry(profile *models.IPProfile, threshold float64) string {
	total := 0
	for _, cnt := range profile.Countries {
		total += cnt
	}
	if total == 0 {
		return ""
	}
	best, bestCnt := "", 0
	for cc, cnt := range profile.Countries {
		if cnt > bestCnt {
			best, bestCnt = cc, cnt
		}
	}
	if float64(bestCnt)/float64(total) >= threshold {
		return best
	}
	return ""
}
