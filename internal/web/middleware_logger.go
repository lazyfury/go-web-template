package web

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// Logger returns a zap-based access-log middleware. Place it after RequestID
// so every log line carries the request id.
func Logger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		if raw != "" {
			path = path + "?" + raw
		}

		c.Next()

		fields := []zap.Field{
			zap.Int("status", c.Writer.Status()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Duration("latency", time.Since(start)),
			zap.String("request_id", httpx.RequestIDFromContext(c.Request.Context())),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("error", c.Errors.String()))
		}
		// Authenticated routes set the principal; anonymous routes omit user_id.
		if principal, ok := authkit.PrincipalFrom(c); ok {
			fields = append(fields, zap.Uint64("user_id", principal.UserID))
		}
		logger.Info("http_request", fields...)
	}
}
