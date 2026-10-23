package web

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout injects a deadline into the request context for the /api/v1 chain.
//
// It deliberately does not run the handler in a separate goroutine: gin.Context
// is not safe for concurrent use, so racing a synthetic "timeout response"
// against the handler is a data race (and was observed under -race). Instead
// the deadline propagates through c.Request.Context() into services and
// database calls; handlers/repositories must honor ctx. Hard transport-level
// limits stay with http.Server's read/write timeouts.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
