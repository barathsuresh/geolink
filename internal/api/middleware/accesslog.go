// internal/api/middleware/accesslog.go
// Gin middleware: structured JSON access log via slog.
package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog logs one structured JSON line per request: method, path, status,
// latency, ip, and request_id. Compatible with CloudWatch Logs Insights queries.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		reqID, _ := c.Get(RequestIDKey)

		slog.Info("request",
			"method",     c.Request.Method,
			"path",       c.Request.URL.Path,
			"status",     c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip",         c.GetString("client_ip"),
			"request_id", reqID,
			"bytes",      c.Writer.Size(),
		)
	}
}
