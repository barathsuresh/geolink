// internal/api/handler/toggle.go
// PUT /api/v1/toggle/global and PUT /api/v1/toggle/ip — admin personalization toggles.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/barathsuresh/geolink/internal/models"
	"github.com/barathsuresh/geolink/internal/personalization"
)

// ToggleDeps bundles dependencies for the toggle handlers.
type ToggleDeps struct {
	RDB *redis.Client
}

// ToggleGlobal handles PUT /api/v1/toggle/global.
func ToggleGlobal(deps ToggleDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.ToggleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
				"code":  "INVALID_REQUEST",
			})
			return
		}

		if err := personalization.SetGlobalToggle(c.Request.Context(), deps.RDB, req.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update global toggle",
				"code":  "INTERNAL_ERROR",
			})
			return
		}

		c.JSON(http.StatusOK, models.ToggleResponse{
			Scope:   "global",
			Enabled: req.Enabled,
		})
	}
}

// ToggleIP handles PUT /api/v1/toggle/ip.
func ToggleIP(deps ToggleDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.ToggleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
				"code":  "INVALID_REQUEST",
			})
			return
		}

		if req.IP == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "ip field is required for per-IP toggle",
				"code":  "MISSING_IP",
			})
			return
		}

		if err := personalization.SetIPToggle(c.Request.Context(), deps.RDB, req.IP, req.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update IP toggle",
				"code":  "INTERNAL_ERROR",
			})
			return
		}

		c.JSON(http.StatusOK, models.ToggleResponse{
			Scope:   "ip",
			IP:      req.IP,
			Enabled: req.Enabled,
		})
	}
}
