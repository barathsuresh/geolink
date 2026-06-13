// internal/api/middleware/ipextract.go
// Gin middleware: extracts the real client IP and stores it in the request context.
package middleware

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

// IPExtract extracts the real client IP. It checks the 'ip' query parameter first (for local dev override),
// then X-Forwarded-For, and falls back to RemoteAddr. Stores the result at context key "client_ip".
func IPExtract() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Query("ip")

		if ip == "" {
			if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
				// Take only the first (leftmost) IP in a comma-separated list.
				parts := strings.SplitN(xff, ",", 2)
				ip = strings.TrimSpace(parts[0])
			}
		}

		if ip == "" {
			// Strip port from RemoteAddr (e.g. "1.2.3.4:54321").
			if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
				ip = host
			} else {
				ip = c.Request.RemoteAddr
			}
		}

		c.Set("client_ip", ip)
		c.Next()
	}
}
