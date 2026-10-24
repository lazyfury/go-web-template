package web

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// defaultSweepInterval is how often Run evicts buckets that have been idle for
// two intervals. Login bursts create one bucket per source IP, so without the
// sweep a long-running process accumulates map entries forever.
const defaultSweepInterval = 10 * time.Minute

// LimiterOption customizes the Limiters registry.
type LimiterOption func(*Limiters)

// WithLimiterSweepInterval overrides the idle-bucket sweep interval. Tests use a
// short interval; production keeps the default.
func WithLimiterSweepInterval(interval time.Duration) LimiterOption {
	return func(l *Limiters) {
		if interval > 0 {
			l.sweepInterval = interval
		}
	}
}

// WithLimiterClock injects the clock used for idle detection. Token-bucket
// refill itself stays on the real clock (golang.org/x/time/rate).
func WithLimiterClock(now func() time.Time) LimiterOption {
	return func(l *Limiters) {
		if now != nil {
			l.now = now
		}
	}
}

// Limiters owns every token-bucket limiter of the process.
//
// Why a registry instead of a self-starting limiter: ADR 14 (context.md) only
// allows root goroutines in cmd/server. The sweep of idle buckets used to be a
// fire-and-forget goroutine started from a constructor, which meant its
// lifetime was unowned and it survived (and kept running) after the router was
// rebuilt in tests. Now the constructor is inert and cmd/server calls Run.
type Limiters struct {
	mu            sync.Mutex
	sweepInterval time.Duration
	now           func() time.Time
	sets          []*bucketSet
}

// NewLimiters creates an inert registry. Nothing is started until Run is called.
func NewLimiters(opts ...LimiterOption) *Limiters {
	l := &Limiters{
		sweepInterval: defaultSweepInterval,
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Middleware registers one limiter and returns the middleware enforcing it.
// When enabled is false it registers nothing and returns a no-op, so the same
// router code works in every environment.
//
// keyFn decides the bucket dimension: RateLimit-style guards key by client IP
// (c.ClientIP), the password-change guard keys by user id.
func (l *Limiters) Middleware(enabled bool, keyFn func(*gin.Context) string, rps float64, burst int) gin.HandlerFunc {
	if !enabled {
		return func(c *gin.Context) { c.Next() }
	}
	set := &bucketSet{
		clients: make(map[string]*clientBucket),
		rps:     rate.Limit(rps),
		burst:   burst,
		now:     l.now,
	}
	l.mu.Lock()
	l.sets = append(l.sets, set)
	l.mu.Unlock()

	return func(c *gin.Context) {
		if !set.allow(keyFn(c)) {
			c.Header("Retry-After", strconv.Itoa(1))
			httpx.Error(c, http.StatusTooManyRequests, apperr.CodeTooManyRequests, "too many requests, please slow down")
			return
		}
		c.Next()
	}
}

// Len reports how many limiters are registered (used by cmd/server to decide
// whether the sweeper is worth a goroutine, and by tests).
func (l *Limiters) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.sets)
}

// Run sweeps idle buckets until ctx is canceled. It returns nil on cancellation:
// it is a janitor, not a failure source. It returns immediately when no limiter
// was registered.
func (l *Limiters) Run(ctx context.Context) error {
	if l.Len() == 0 {
		return nil
	}
	ticker := time.NewTicker(l.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			l.Sweep()
		}
	}
}

// Sweep drops every bucket idle for more than two sweep intervals. Exported for
// tests and operational tooling; production relies on Run.
func (l *Limiters) Sweep() {
	cutoff := l.now().Add(-2 * l.sweepInterval)
	l.mu.Lock()
	sets := make([]*bucketSet, len(l.sets))
	copy(sets, l.sets)
	l.mu.Unlock()
	for _, set := range sets {
		set.sweep(cutoff)
	}
}

// bucketSet is one token bucket per key.
type bucketSet struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	rps     rate.Limit
	burst   int
	now     func() time.Time
}

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func (s *bucketSet) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	bucket, ok := s.clients[key]
	if !ok {
		bucket = &clientBucket{limiter: rate.NewLimiter(s.rps, s.burst)}
		s.clients[key] = bucket
	}
	bucket.lastSeen = s.now()
	return bucket.limiter.Allow()
}

// sweep drops buckets last seen before cutoff.
func (s *bucketSet) sweep(cutoff time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, bucket := range s.clients {
		if bucket.lastSeen.Before(cutoff) {
			delete(s.clients, key)
		}
	}
}
