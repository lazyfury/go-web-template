package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/response"
)

// ipRateLimiter tracks a token bucket per client IP.
type ipRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	rps     rate.Limit
	burst   int
}

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates a per-IP rate limiter. Idle buckets are evicted
// after cleanupEvery to bound memory usage.
func NewIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	rl := &ipRateLimiter{
		clients: make(map[string]*clientBucket),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
	go rl.cleanupLoop(10 * time.Minute)
	return rl
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, ok := rl.clients[ip]
	if !ok {
		bucket = &clientBucket{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.clients[ip] = bucket
	}
	bucket.lastSeen = time.Now()
	return bucket.limiter.Allow()
}

func (rl *ipRateLimiter) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-interval * 2)
		for ip, bucket := range rl.clients {
			if bucket.lastSeen.Before(cutoff) {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimit returns a middleware enforcing per-IP rate limits. When disabled
// it is a no-op, so the same router code works in every environment.
func RateLimit(enabled bool, rps float64, burst int) gin.HandlerFunc {
	if !enabled {
		return func(c *gin.Context) { c.Next() }
	}
	limiter := NewIPRateLimiter(rps, burst)
	return func(c *gin.Context) {
		ip := clientIP(c)
		if !limiter.allow(ip) {
			c.Header("Retry-After", strconv.Itoa(1))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, response.Body{
				Code:    apperrors.CodeTooManyRequests,
				Message: "too many requests, please slow down",
			})
			return
		}
		c.Next()
	}
}

// clientIP resolves the real client IP behind a single reverse proxy.
func clientIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		// X-Forwarded-For may be "client, proxy1, proxy2" — take the first.
		if idx := strings.IndexByte(ip, ','); idx >= 0 {
			ip = ip[:idx]
		}
		if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
			return parsed.String()
		}
	}
	return c.ClientIP()
}
