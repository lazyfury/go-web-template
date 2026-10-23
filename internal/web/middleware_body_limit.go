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
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 && c.Request.Body != nil {
			if c.Request.ContentLength > maxBytes {
				httpx.Error(c, http.StatusRequestEntityTooLarge, apperr.CodePayloadTooLarge, "request body too large")
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
