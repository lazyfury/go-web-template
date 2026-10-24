package web

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/example/go-web-template/internal/platform/httpx"
)

// RequestIDHeader is the header used to propagate the request id.
const RequestIDHeader = httpx.RequestIDHeader

// maxRequestIDLen bounds a client supplied id so one caller cannot push
// arbitrarily long strings into every log line and error response.
const maxRequestIDLen = 64

// validRequestID accepts the conservative character set used by the ids we care
// about (UUID, W3C traceparent-ish, nginx $request_id). Anything else — spaces,
// quotes, control characters, unicode look-alikes — is discarded and replaced
// with a server generated id, so a caller cannot choose how the log looks.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

// RequestID injects or reads an X-Request-ID header and stores it in the
// request context so loggers and error responses can correlate a request.
// A client supplied id is only honored when it is well formed; otherwise the
// caller could forge correlation ids and stuff arbitrary text into the logs.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		c.Request = c.Request.WithContext(httpx.WithRequestID(c.Request.Context(), id))
		c.Writer.Header().Set(RequestIDHeader, id)
		c.Next()
	}
}
