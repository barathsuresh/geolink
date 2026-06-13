// internal/personalization/toggle.go
// Redis-backed global and per-IP personalization toggle.
package personalization

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	keyGlobal = "personalization:global"
)

func keyIPToggle(ip string) string {
	return fmt.Sprintf("personalization:ip:%s", ip)
}

// IsPersonalizationEnabled returns true if both the global toggle AND the per-IP
// toggle are not set to "false". Defaults to enabled when the key is absent.
func IsPersonalizationEnabled(ctx context.Context, rdb *redis.Client, ip string) bool {
	// Check global flag.
	global, err := rdb.Get(ctx, keyGlobal).Result()
	if err == nil && global == "false" {
		return false
	}

	// Check per-IP flag.
	ipFlag, err := rdb.Get(ctx, keyIPToggle(ip)).Result()
	if err == nil && ipFlag == "false" {
		return false
	}

	return true
}

// SetGlobalToggle persists the global personalization flag to Redis.
func SetGlobalToggle(ctx context.Context, rdb *redis.Client, enabled bool) error {
	val := "true"
	if !enabled {
		val = "false"
	}
	return rdb.Set(ctx, keyGlobal, val, 0).Err()
}

// SetIPToggle persists a per-IP personalization flag to Redis.
func SetIPToggle(ctx context.Context, rdb *redis.Client, ip string, enabled bool) error {
	val := "true"
	if !enabled {
		val = "false"
	}
	return rdb.Set(ctx, keyIPToggle(ip), val, 0).Err()
}
