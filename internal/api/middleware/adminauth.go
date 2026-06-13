// internal/api/middleware/adminauth.go
// Gin middleware: validates the X-Admin-Key header for protected endpoints.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/barathsuresh/geolink/internal/config"
)

// AdminAuth returns a middleware that enforces a static API key header check.
// Returns 401 if the header is missing, 403 if it is present but wrong.
// Panics at startup if ADMIN_API_KEY is not configured — prevents silent open access.
func AdminAuth(cfg *config.Config) gin.HandlerFunc {
	if strings.TrimSpace(cfg.AdminAPIKey) == "" {
		panic("ADMIN_API_KEY must be set before starting the server")
	}
	return func(c *gin.Context) {
		key := c.GetHeader("X-Admin-Key")

		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "missing X-Admin-Key header",
				"code":  "UNAUTHORIZED",
			})
			c.Abort()
			return
		}

		if key != cfg.AdminAPIKey {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid admin key",
				"code":  "FORBIDDEN",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
