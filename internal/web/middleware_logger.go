package web

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// loggableQueryKeys is the allowlist of query parameters that may appear in the
// access log. Free-text parameters (keyword, q, email, ...) are user input and
// may be personal data, so they never reach the log; unknown keys are dropped
// silently. Everything else (paging, filters, sort) is an enum or a number.
var loggableQueryKeys = map[string]struct{}{
	"page":      {},
	"page_size": {},
	"status":    {},
	"channel":   {},
	"level":     {},
	"unread":    {},
	"read":      {},
	"sort":      {},
	"order":     {},
}

// loggerQueryValueLimit bounds each logged value: an attacker can put anything
// in an allowlisted key, and a megabyte-long query would otherwise be written
// verbatim into every access log line.
const loggerQueryValueLimit = 32

// sanitizedQuery rebuilds the query string from allowlisted keys only. An
// unparsable query is dropped instead of being logged raw.
func sanitizedQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if _, ok := loggableQueryKeys[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var parts []string
	for _, key := range keys {
		for _, value := range values[key] {
			if len(value) > loggerQueryValueLimit {
				value = value[:loggerQueryValueLimit]
			}
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, "&")
}

// Logger returns a zap-based access-log middleware. Place it after RequestID
// so every log line carries the request id.
func Logger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		if query := sanitizedQuery(c.Request.URL.RawQuery); query != "" {
			path = path + "?" + query
		}
		// Expose the trace id on the response (next to X-Request-ID) so a user
		// report can be pasted straight into Tempo/Jaeger. The header must be set
		// before the handler writes the response.
		spanContext := trace.SpanContextFromContext(c.Request.Context())
		if spanContext.IsValid() {
			c.Header("X-Trace-Id", spanContext.TraceID().String())
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
		if spanContext.IsValid() {
			fields = append(fields, zap.String("trace_id", spanContext.TraceID().String()))
		}
		logger.Info("http_request", fields...)
	}
}
