package web

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// defaultSweepInterval is how often Run evicts buckets that have been idle for
// two intervals. Login bursts create one bucket per source IP, so without the
// sweep a long-running process accumulates map entries forever.
const defaultSweepInterval = 10 * time.Minute

// storeErrorLogInterval bounds how often a broken store is reported: during a
// Redis outage every request fails the same way, and logging each one would
// turn a degraded system into a log flood.
const storeErrorLogInterval = time.Minute

// RateLimitStore is the counter behind the limiters. The in-memory
// implementation lives in this package; internal/platform/redisx provides one
// backed by Redis so several replicas share the same buckets.
//
// Allow consumes one token for key in the named bucket and reports whether the
// request may proceed. A non-nil error means the store is unavailable: the
// middleware fails open and logs it, because a rate limiter is a protection
// layer, not an authority on who may log in.
type RateLimitStore interface {
	Allow(ctx context.Context, bucket, key string, rps float64, burst int) (bool, error)
}

// SweepStore is implemented by stores that keep local state which must be
// evicted (the in-memory bucket map). Redis-backed stores do not implement it:
// Redis expires their keys by itself.
type SweepStore interface {
	Sweep(olderThan time.Duration)
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
	store         RateLimitStore
	logger        *zap.Logger
	count         int
	lastErrorLog  map[string]time.Time
}

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

// WithRateLimitStore replaces the default in-memory store, e.g. with the
// Redis-backed one so every replica shares the same buckets.
func WithRateLimitStore(store RateLimitStore) LimiterOption {
	return func(l *Limiters) {
		if store != nil {
			l.store = store
		}
	}
}

// WithLimiterLogger sets the logger used for store failures.
func WithLimiterLogger(logger *zap.Logger) LimiterOption {
	return func(l *Limiters) {
		if logger != nil {
			l.logger = logger
		}
	}
}

// NewLimiters creates an inert registry. Nothing is started until Run is called.
func NewLimiters(opts ...LimiterOption) *Limiters {
	l := &Limiters{
		sweepInterval: defaultSweepInterval,
		now:           time.Now,
		logger:        zap.NewNop(),
		lastErrorLog:  make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.store == nil {
		l.store = newMemoryRateLimitStore(l.now)
	}
	return l
}

// Middleware registers one limiter and returns the middleware enforcing it.
// When enabled is false it registers nothing and returns a no-op, so the same
// router code works in every environment.
//
// name namespaces the bucket (Redis keys are "ratelimit:<name>:<key>", so the
// login guard and the password guard never share counters). keyFn decides the
// dimension within that namespace: client IP for the login guard, user id for
// the password-change guard.
func (l *Limiters) Middleware(name string, enabled bool, keyFn func(*gin.Context) string, rps float64, burst int) gin.HandlerFunc {
	if !enabled {
		return func(c *gin.Context) { c.Next() }
	}
	l.mu.Lock()
	l.count++
	l.mu.Unlock()

	// A constant "1" lies to clients on slow buckets (the password guard refills
	// one token every auth.password_rps seconds); tell them when a token is
	// actually available again. Capped so a tiny rps cannot park a client for
	// minutes on one request.
	retryAfter := 1
	if rps > 0 {
		if seconds := int(math.Ceil(1 / rps)); seconds > retryAfter {
			retryAfter = min(seconds, 60)
		}
	}

	return func(c *gin.Context) {
		allowed, err := l.store.Allow(c.Request.Context(), name, keyFn(c), rps, burst)
		if err != nil {
			// Fail open: a Redis outage must not 429 the whole API. The error is
			// logged (rate-limited) and counted, so the outage is visible instead
			// of silently disabling the limiter.
			rateLimitStoreErrors.WithLabelValues(name).Inc()
			l.logStoreError(name, err)
		} else if !allowed {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
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
	return l.count
}

// Run sweeps idle buckets until ctx is canceled. It returns nil on cancellation:
// it is a janitor, not a failure source. It returns immediately when no limiter
// was registered or when the store has no local state to evict (Redis).
func (l *Limiters) Run(ctx context.Context) error {
	if l.Len() == 0 {
		return nil
	}
	store, ok := l.store.(SweepStore)
	if !ok {
		return nil
	}
	ticker := time.NewTicker(l.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			store.Sweep(2 * l.sweepInterval)
		}
	}
}

// Sweep evicts local buckets idle for longer than two sweep intervals. Exported
// for tests and operational tooling; production relies on Run.
func (l *Limiters) Sweep() {
	if store, ok := l.store.(SweepStore); ok {
		store.Sweep(2 * l.sweepInterval)
	}
}

func (l *Limiters) logStoreError(name string, err error) {
	l.mu.Lock()
	now := l.now()
	last, seen := l.lastErrorLog[name]
	if seen && now.Sub(last) < storeErrorLogInterval {
		l.mu.Unlock()
		return
	}
	l.lastErrorLog[name] = now
	l.mu.Unlock()
	l.logger.Error("rate limit store unavailable; failing open",
		zap.String("bucket", name),
		zap.Error(err),
	)
}

var rateLimitStoreErrors = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ratelimit_store_errors_total",
		Help: "Rate-limit store failures (the limiter failed open), by bucket.",
	},
	[]string{"bucket"},
)

func init() {
	prometheus.MustRegister(rateLimitStoreErrors)
}

// memoryRateLimitStore is the default single-instance store: one token bucket
// per (bucket, key), refilled by golang.org/x/time/rate.
type memoryRateLimitStore struct {
	mu      sync.Mutex
	clock   func() time.Time
	buckets map[string]map[string]*clientBucket
}

func newMemoryRateLimitStore(clock func() time.Time) *memoryRateLimitStore {
	return &memoryRateLimitStore{
		clock:   clock,
		buckets: make(map[string]map[string]*clientBucket),
	}
}

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func (s *memoryRateLimitStore) Allow(_ context.Context, bucket, key string, rps float64, burst int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clients, ok := s.buckets[bucket]
	if !ok {
		clients = make(map[string]*clientBucket)
		s.buckets[bucket] = clients
	}
	entry, ok := clients[key]
	if !ok {
		entry = &clientBucket{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
		clients[key] = entry
	}
	entry.lastSeen = s.clock()
	return entry.limiter.Allow(), nil
}

// Sweep drops buckets last seen before now-olderThan.
func (s *memoryRateLimitStore) Sweep(olderThan time.Duration) {
	cutoff := s.clock().Add(-olderThan)
	s.mu.Lock()
	defer s.mu.Unlock()

	for bucket, clients := range s.buckets {
		for key, entry := range clients {
			if entry.lastSeen.Before(cutoff) {
				delete(clients, key)
			}
		}
		if len(clients) == 0 {
			delete(s.buckets, bucket)
		}
	}
}
