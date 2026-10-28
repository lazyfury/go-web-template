package web

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// BodyLimit caps the request body size. Requests that declare a larger
// Content-Length are rejected up front with 413; chunked bodies are capped by
// http.MaxBytesReader so they cannot stream past the limit either.
//
// The default budget is sized for JSON payloads (1 MiB). Upload routes get
// their own, bigger cap through overrides, keyed by the gin route pattern
// (c.FullPath()); the middleware runs after routing, so the pattern is already
// known here.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return BodyLimitOverrides(maxBytes, nil)
}

// BodyLimitOverrides is BodyLimit with per-route caps. An override only ever
// raises the limit: a route cannot opt out of the global cap, and unknown
// patterns are ignored.
func BodyLimitOverrides(maxBytes int64, overrides map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := maxBytes
		if pattern := c.FullPath(); pattern != "" {
			if override, ok := overrides[pattern]; ok && override > limit {
				limit = override
			}
		}
		if limit > 0 && c.Request.Body != nil {
			if c.Request.ContentLength > limit {
				httpx.Error(c, http.StatusRequestEntityTooLarge, apperr.CodePayloadTooLarge, "request body too large")
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}
