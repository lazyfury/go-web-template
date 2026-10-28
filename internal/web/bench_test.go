package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// BenchmarkMemoryRateLimitStoreAllow measures the default single-replica store:
// one mutex + one token-bucket refill per request.
func BenchmarkMemoryRateLimitStoreAllow(b *testing.B) {
	store := newMemoryRateLimitStore(time.Now)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if allowed, err := store.Allow(context.Background(), "bench", "203.0.113.7", 1e9, 1e9); err != nil || !allowed {
			b.Fatalf("Allow: allowed=%v err=%v", allowed, err)
		}
	}
}

// BenchmarkRateLimitMiddleware measures the whole guard (gin context + store)
// as it sits on every /api/v1 request.
func BenchmarkRateLimitMiddleware(b *testing.B) {
	gin.SetMode(gin.TestMode)
	limiters := NewLimiters()
	engine := gin.New()
	engine.Use(limiters.Middleware("bench", true, func(c *gin.Context) string { return c.ClientIP() }, 1e9, 1e9))
	engine.GET("/api/v1/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/ping", http.NoBody)
	request.RemoteAddr = "203.0.113.7:12345"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
	}
}
