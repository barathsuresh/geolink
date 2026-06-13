// internal/api/middleware/helpers.go
// Shared helpers used by multiple middleware functions.
package middleware

import "time"

// secondsDuration converts an integer number of seconds to a time.Duration.
func secondsDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}
