// internal/api/middleware/ratelimit.go
// Gin middleware: per-IP fixed-window rate limiter backed by an atomic Redis Lua script.
package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/barathsuresh/geolink/internal/config"
)

// rateLimitScript atomically increments the counter and sets TTL on first hit.
// INCR then EXPIRE is not atomic — two concurrent requests can both see count=1
// and only one sets the TTL, causing the window to never expire.
// This Lua script runs atomically on the Redis server, fixing the race.
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

// RateLimit returns a middleware that enforces a per-IP request-velocity limit.
// Uses an atomic Lua script keyed by "velocity:{ip}" with a fixed window TTL.
func RateLimit(rdb *redis.Client, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip, _ := c.Get("client_ip")
		ipStr, _ := ip.(string)
		key := fmt.Sprintf("velocity:%s", ipStr)

		ctx := c.Request.Context()

		res, err := rateLimitScript.Run(ctx, rdb,
			[]string{key},
			cfg.VelocityWindowSeconds,
		).Int()
		if err != nil {
			// Redis unavailable — fail open.
			c.Next()
			return
		}

		if res > cfg.VelocityLimit {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":               "rate limit exceeded",
				"code":                "RATE_LIMITED",
				"retry_after_seconds": cfg.VelocityWindowSeconds,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
