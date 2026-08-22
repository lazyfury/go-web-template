package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/response"
)

// timeoutWriter wraps the response writer so a timed-out request cannot write
// to the client after the deadline fired.
type timeoutWriter struct {
	gin.ResponseWriter
	mu        sync.Mutex
	timedOut  bool
	headerSet bool
}

func (w *timeoutWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return
	}
	w.headerSet = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *timeoutWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !w.headerSet {
		w.headerSet = true
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *timeoutWriter) WriteString(s string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !w.headerSet {
		w.headerSet = true
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.WriteString(s)
}

// Timeout bounds the total handler execution time per request. On timeout the
// client receives a 504 and the handler goroutine keeps running until it
// returns (it can no longer write to the client).
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		writer := &timeoutWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Request = c.Request.WithContext(ctx)

		done := make(chan struct{})
		go func() {
			defer close(done)
			c.Next()
		}()

		select {
		case <-done:
			// Handler finished within the deadline.
		case <-ctx.Done():
			writer.mu.Lock()
			writer.timedOut = true
			writer.mu.Unlock()
			c.AbortWithStatusJSON(http.StatusGatewayTimeout, response.Body{
				Code:    apperrors.CodeInternal,
				Message: "request timed out",
			})
		}
	}
}
