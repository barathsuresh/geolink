// internal/api/handler/health.go
// GET /api/v1/health — liveness and readiness probe.
package handler

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/typesense/typesense-go/typesense"
)

// HealthDeps bundles the three infrastructure clients to probe.
type HealthDeps struct {
	Pool     *pgxpool.Pool
	RDB      *redis.Client
	TSClient *typesense.Client
}

// Health handles GET /api/v1/health.
// All three probes run concurrently — worst-case latency is max(probe) not sum(probes).
// Returns 200 if all up; 503 if any down.
func Health(deps HealthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var (
			mu              sync.Mutex
			postgresStatus  = "ok"
			redisStatus     = "ok"
			typesenseStatus = "ok"
			wg              sync.WaitGroup
		)

		wg.Add(3)

		go func() {
			defer wg.Done()
			if err := deps.Pool.Ping(ctx); err != nil {
				mu.Lock()
				postgresStatus = "error"
				mu.Unlock()
			}
		}()

		go func() {
			defer wg.Done()
			if err := deps.RDB.Ping(ctx).Err(); err != nil {
				mu.Lock()
				redisStatus = "error"
				mu.Unlock()
			}
		}()

		go func() {
			defer wg.Done()
			healthy, err := deps.TSClient.Health(ctx, 5*time.Second)
			if err != nil || !healthy {
				mu.Lock()
				typesenseStatus = "error"
				mu.Unlock()
			}
		}()

		wg.Wait()

		allOK := postgresStatus == "ok" && redisStatus == "ok" && typesenseStatus == "ok"
		status := http.StatusOK
		if !allOK {
			status = http.StatusServiceUnavailable
		}

		c.JSON(status, gin.H{
			"status":    boolStatus(allOK),
			"postgres":  postgresStatus,
			"redis":     redisStatus,
			"typesense": typesenseStatus,
		})
	}
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "degraded"
}
