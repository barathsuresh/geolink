// internal/personalization/profile.go
// Redis-backed IP profile reader and writer.
package personalization

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/models"
)

// key helpers
func keyCountries(ip string) string    { return fmt.Sprintf("ip:%s:countries", ip) }
func keyContinents(ip string) string   { return fmt.Sprintf("ip:%s:continents", ip) }
func keyFeatureCodes(ip string) string { return fmt.Sprintf("ip:%s:feature_codes", ip) }
func keyRecent(ip string) string       { return fmt.Sprintf("ip:%s:recent", ip) }

// GetProfile reads all four profile keys for the given IP in a single pipeline.
// Returns (nil, nil) if the profile is empty — no history recorded yet.
func GetProfile(ctx context.Context, rdb *redis.Client, ip string) (*models.IPProfile, error) {
	var (
		countriesCmd    *redis.MapStringStringCmd
		continentsCmd   *redis.MapStringStringCmd
		featureCodesCmd *redis.MapStringStringCmd
		recentCmd       *redis.StringSliceCmd
	)

	_, err := rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		countriesCmd    = pipe.HGetAll(ctx, keyCountries(ip))
		continentsCmd   = pipe.HGetAll(ctx, keyContinents(ip))
		featureCodesCmd = pipe.HGetAll(ctx, keyFeatureCodes(ip))
		recentCmd       = pipe.LRange(ctx, keyRecent(ip), 0, -1)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get profile pipeline: %w", err)
	}

	countries    := parseIntHash(countriesCmd.Val())
	continents   := parseIntHash(continentsCmd.Val())
	featureCodes := parseIntHash(featureCodesCmd.Val())
	recent       := recentCmd.Val()

	// No data across all keys → treat as new user with no history.
	if len(countries) == 0 && len(continents) == 0 && len(featureCodes) == 0 && len(recent) == 0 {
		return nil, nil
	}

	return &models.IPProfile{
		IP:           ip,
		Countries:    countries,
		Continents:   continents,
		FeatureCodes: featureCodes,
		Recent:       recent,
	}, nil
}

// UpdateProfile increments search-signal counters and prepends the query to the
// recent-searches list. All writes are pipelined for minimal round-trips.
func UpdateProfile(
	ctx context.Context,
	rdb *redis.Client,
	cfg *config.Config,
	event models.SearchEvent,
) error {
	ip := event.IP
	ttl := time.Duration(cfg.IPProfileTTLDays) * 24 * time.Hour

	_, err := rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		// Increment frequency counters.
		if event.CountryCode != "" {
			pipe.HIncrBy(ctx, keyCountries(ip), event.CountryCode, 1)
		}
		if event.Continent != "" {
			pipe.HIncrBy(ctx, keyContinents(ip), event.Continent, 1)
		}
		if event.FeatureCode != "" {
			pipe.HIncrBy(ctx, keyFeatureCodes(ip), event.FeatureCode, 1)
		}

		// Prepend query to recent list, then trim to limit.
		pipe.LPush(ctx, keyRecent(ip), event.Query)
		pipe.LTrim(ctx, keyRecent(ip), 0, int64(cfg.RecentSearchesLimit-1))

		// Refresh TTL on all keys.
		pipe.Expire(ctx, keyCountries(ip), ttl)
		pipe.Expire(ctx, keyContinents(ip), ttl)
		pipe.Expire(ctx, keyFeatureCodes(ip), ttl)
		pipe.Expire(ctx, keyRecent(ip), ttl)

		return nil
	})
	if err != nil {
		return fmt.Errorf("update profile pipeline: %w", err)
	}
	return nil
}

// ResetProfile deletes all four profile keys for the given IP.
func ResetProfile(ctx context.Context, rdb *redis.Client, ip string) error {
	return rdb.Del(ctx,
		keyCountries(ip), keyContinents(ip), keyFeatureCodes(ip), keyRecent(ip),
	).Err()
}

// parseIntHash converts a raw Redis HASH map[string]string → map[string]int.
func parseIntHash(raw map[string]string) map[string]int {
	m := make(map[string]int, len(raw))
	for k, v := range raw {
		if n, err := strconv.Atoi(v); err == nil {
			m[k] = n
		}
	}
	return m
}
