// internal/api/router.go
// Gin router factory — wires all routes, middleware, and handler dependencies.
package api

import (
	"strings"

	gzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/typesense/typesense-go/typesense"

	"github.com/barathsuresh/geolink/frontend"
	"github.com/barathsuresh/geolink/internal/api/handler"
	"github.com/barathsuresh/geolink/internal/api/middleware"
	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/kafka"
)

// RouterDeps bundles all dependencies required to build the router.
type RouterDeps struct {
	Cfg      *config.Config
	Pool     *pgxpool.Pool
	RDB      *redis.Client
	TSClient *typesense.Client
	Producer *kafka.Producer
}

// NewRouter builds and returns a configured Gin engine.
func NewRouter(deps RouterDeps) *gin.Engine {
	if deps.Cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.AccessLog())
	r.Use(gzip.Gzip(gzip.DefaultCompression))

	// ── CORS ──────────────────────────────────────────────────────────────────
	// CORS_ALLOWED_ORIGINS: comma-separated list of allowed origins.
	// Dev default "*" permits file:// and localhost without a dev server.
	// Production: set to your actual domain, e.g. "https://app.example.com".
	allowedOrigins := buildOriginSet(deps.Cfg.CORSOrigins)
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if _, wildcard := allowedOrigins["*"]; wildcard {
			c.Header("Access-Control-Allow-Origin", "*")
		} else if origin != "" {
			if _, ok := allowedOrigins[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
		}
		c.Header("Access-Control-Allow-Methods", "GET, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-Admin-Key, X-Forwarded-For")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// ── Frontend (embedded in binary) ────────────────────────────────────────
	r.GET("/", func(c *gin.Context) {
		data, _ := frontend.FS.ReadFile("index.html")
		c.Data(200, "text/html; charset=utf-8", data)
	})

	// ── Public routes (/api/v1) ───────────────────────────────────────────────
	v1 := r.Group("/api/v1")
	v1.Use(middleware.IPExtract())
	{
		// Search — additional per-route rate-limit middleware.
		v1.GET("/search",
			middleware.RateLimit(deps.RDB, deps.Cfg),
			handler.Search(handler.SearchDeps{
				TSClient: deps.TSClient,
				RDB:      deps.RDB,
				Producer: deps.Producer,
				Cfg:      deps.Cfg,
			}),
		)

		// Explicit search event tracking
		v1.POST("/events/search",
			handler.RecordSearchEvent(handler.SearchDeps{
				TSClient: deps.TSClient,
				RDB:      deps.RDB,
				Producer: deps.Producer,
				Cfg:      deps.Cfg,
			}),
		)

		// Analytics (read-only, public).
		v1.GET("/analytics/profile",
			handler.GetProfile(handler.AnalyticsDeps{RDB: deps.RDB}),
		)

		// Health check.
		v1.GET("/health",
			handler.Health(handler.HealthDeps{
				Pool:     deps.Pool,
				RDB:      deps.RDB,
				TSClient: deps.TSClient,
			}),
		)

		// Recommendations — history → geo-IP → global priority.
		v1.GET("/recommendations",
			handler.Recommendations(handler.RecommendationsDeps{
				TSClient:   deps.TSClient,
				RDB:        deps.RDB,
				Cfg:        deps.Cfg,
				Collection: deps.Cfg.TypesenseCollection,
			}),
		)

		// Clear own history.
		v1.DELETE("/profile/reset",
			handler.ResetMyProfile(handler.AnalyticsDeps{RDB: deps.RDB}),
		)
	}

	// ── Protected admin routes ────────────────────────────────────────────────
	admin := r.Group("/api/v1")
	admin.Use(middleware.IPExtract(), middleware.AdminAuth(deps.Cfg))
	{
		admin.PUT("/toggle/global",
			handler.ToggleGlobal(handler.ToggleDeps{RDB: deps.RDB}),
		)
		admin.PUT("/toggle/ip",
			handler.ToggleIP(handler.ToggleDeps{RDB: deps.RDB}),
		)
		admin.DELETE("/analytics/profile/reset",
			handler.ResetProfile(handler.AnalyticsDeps{RDB: deps.RDB}),
		)
	}

	return r
}

// buildOriginSet parses a comma-separated origin list into a lookup map.
func buildOriginSet(raw string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			m[o] = struct{}{}
		}
	}
	return m
}
