// internal/api/handler/analytics.go
// GET /api/v1/analytics/profile and DELETE /api/v1/analytics/profile/reset.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/barathsuresh/geolink/internal/personalization"
)

// AnalyticsDeps bundles dependencies for the analytics handlers.
type AnalyticsDeps struct {
	RDB *redis.Client
}

// GetProfile handles GET /api/v1/analytics/profile?ip=x.x.x.x.
func GetProfile(deps AnalyticsDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Query("ip")
		if ip == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "ip query parameter is required",
				"code":  "MISSING_IP",
			})
			return
		}

		profile, err := personalization.GetProfile(c.Request.Context(), deps.RDB, ip)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to read profile",
				"code":  "INTERNAL_ERROR",
			})
			return
		}

		if profile == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "no profile found for this IP",
				"code":  "NOT_FOUND",
			})
			return
		}

		c.JSON(http.StatusOK, profile)
	}
}

// ResetProfile handles DELETE /api/v1/analytics/profile/reset?ip=x.x.x.x.
func ResetProfile(deps AnalyticsDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Query("ip")
		if ip == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "ip query parameter is required",
				"code":  "MISSING_IP",
			})
			return
		}

		if err := personalization.ResetProfile(c.Request.Context(), deps.RDB, ip); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to reset profile",
				"code":  "INTERNAL_ERROR",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "profile reset successfully",
			"ip":      ip,
		})
	}
}

// ResetMyProfile handles DELETE /api/v1/profile/reset.
// Resets the profile for the caller's own IP (extracted from context).
func ResetMyProfile(deps AnalyticsDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP, _ := c.Get("client_ip")
		ip, ok := clientIP.(string)
		if !ok || ip == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "could not determine client IP",
				"code":  "UNKNOWN_IP",
			})
			return
		}

		if err := personalization.ResetProfile(c.Request.Context(), deps.RDB, ip); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to clear history",
				"code":  "INTERNAL_ERROR",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "history cleared successfully",
			"ip":      ip,
		})
	}
}
