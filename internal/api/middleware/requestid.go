// internal/api/middleware/requestid.go
// Gin middleware: assigns a unique X-Request-ID to every request for log correlation.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const RequestIDKey = "request_id"

// RequestID injects a unique request ID into the context and response headers.
// Uses the incoming X-Request-ID header if present (ALB/upstream may set it),
// otherwise generates a new UUID.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		c.Set(RequestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}
