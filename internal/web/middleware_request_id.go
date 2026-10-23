package web

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/example/go-web-template/internal/platform/httpx"
)

// RequestIDHeader is the header used to propagate the request id.
const RequestIDHeader = httpx.RequestIDHeader

// RequestID injects or reads an X-Request-ID header and stores it in the
// request context so loggers and error responses can correlate a request.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Request = c.Request.WithContext(httpx.WithRequestID(c.Request.Context(), id))
		c.Writer.Header().Set(RequestIDHeader, id)
		c.Next()
	}
}
