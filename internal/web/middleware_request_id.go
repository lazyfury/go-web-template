// Package middleware contains gin middlewares shared by all routes.
package web

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader is the header used to propagate the request id.
const RequestIDHeader = "X-Request-ID"

type ctxKey struct{}

// RequestID injects or reads an X-Request-ID header and stores it in the
// request context so loggers and error handlers can correlate a request.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxKey{}, id))
		c.Writer.Header().Set(RequestIDHeader, id)
		c.Next()
	}
}

// RequestIDFromContext returns the request id stored by the RequestID
// middleware, or an empty string when absent.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}
