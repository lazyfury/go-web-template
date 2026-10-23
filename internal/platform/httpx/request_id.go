// Package httpx provides the shared request-id context plumbing so the
// transport layer and the response envelope agree on one key.
package httpx

import "context"

// RequestIDHeader is the header used to propagate the request id.
const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

// WithRequestID returns a context carrying the request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the request id stored by the RequestID
// middleware, or an empty string when absent.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
