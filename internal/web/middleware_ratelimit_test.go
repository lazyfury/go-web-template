package web

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/testutil"
)

// fakeClock is a settable clock for the idle sweep (token-bucket refill keeps
// using the real clock, which the tests never depend on because rps = 0).
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newSweepTestRouter(limiters *Limiters) *gin.Engine {
	r := gin.New()
	// rps = 0: the single burst token is never refilled, so "200" after the
	// first request can only mean the bucket was evicted.
	r.Use(limiters.Middleware("test", true, clientIPKey, 0, 1))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func hit(t *testing.T, r *gin.Engine, addr string) int {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/")
	req.RemoteAddr = addr
	return serve(t, r, req).Code
}

// TestLimitersSweepOnlyWhenCalled 钉住 ADR 14：构造限流器不启动任何 goroutine，
// 空闲桶只在显式 Sweep/Run 时回收。修复前构造函数里藏着 fire-and-forget 的
// cleanupLoop，既没有owner，也无法在测试里断言。
func TestLimitersSweepOnlyWhenCalled(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	limiters := NewLimiters(WithLimiterSweepInterval(time.Minute), WithLimiterClock(clock.Now))
	r := newSweepTestRouter(limiters)

	if code := hit(t, r, "203.0.113.30:1111"); code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", code)
	}
	if code := hit(t, r, "203.0.113.30:2222"); code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", code)
	}

	// Move the injected clock (a hidden 10m sweeper would never fire in a test,
	// which is exactly why the old design could not be asserted). No request is
	// sent in between: the bucket must still be there, so the next request is
	// still throttled.
	clock.Advance(24 * time.Hour)
	time.Sleep(20 * time.Millisecond)
	if code := hit(t, r, "203.0.113.30:3333"); code != http.StatusTooManyRequests {
		t.Fatalf("bucket disappeared without Run/Sweep: status = %d, want 429", code)
	}

	// That request refreshed lastSeen, so idle it again before sweeping.
	clock.Advance(24 * time.Hour)
	limiters.Sweep()

	if code := hit(t, r, "203.0.113.30:4444"); code != http.StatusOK {
		t.Fatalf("bucket survived Sweep: status = %d, want 200", code)
	}
}

// TestLimitersRunSweepsAndStops 保证 Run 真的在扫，并且能被 ctx 取消后返回 nil
// （它是 janitor，不是故障源）。
func TestLimitersRunSweepsAndStops(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	limiters := NewLimiters(WithLimiterSweepInterval(time.Millisecond), WithLimiterClock(clock.Now))
	r := newSweepTestRouter(limiters)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- limiters.Run(ctx) }()

	if code := hit(t, r, "198.51.100.5:1111"); code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", code)
	}
	if code := hit(t, r, "198.51.100.5:2222"); code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", code)
	}

	// Every probe request refreshes lastSeen, so each attempt moves the clock
	// past the idle window first and then gives the ticker a moment to fire.
	deadline := time.Now().Add(2 * time.Second)
	for {
		clock.Advance(24 * time.Hour)
		time.Sleep(5 * time.Millisecond)
		if hit(t, r, "198.51.100.5:3333") == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Run did not sweep the idle bucket")
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestLimitersRunWithoutLimitersReturnsImmediately 保证没有限流器时不留一个空转
// 的 ticker goroutine（cmd/server 可以无条件 group.Go）。
func TestLimitersRunWithoutLimitersReturnsImmediately(t *testing.T) {
	t.Parallel()

	if l := NewLimiters(); l.Len() != 0 {
		t.Fatalf("Len = %d, want 0", l.Len())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- NewLimiters().Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run blocked with no registered limiters")
	}
}

// failingStore stands in for an unreachable Redis.
type failingStore struct{ err error }

func (f failingStore) Allow(context.Context, string, string, float64, int) (bool, error) {
	return false, f.err
}

// TestLimiterFailsOpenWhenTheStoreIsDown 钉住可用性取舍：限流是保护层，不是
// 放行权威。Redis 挂掉时宁可放行并大声报错（计数 + 限频日志），也不要把整个
// API 变成 429。账号锁定仍在数据库里，认证安全不依赖 Redis。
func TestLimiterFailsOpenWhenTheStoreIsDown(t *testing.T) {
	t.Parallel()

	before := testutil.CounterValueFor(t, "ratelimit_store_errors_total", "bucket", "login")
	limiters := NewLimiters(WithRateLimitStore(failingStore{err: errors.New("redis down")}))
	r := gin.New()
	r.Use(limiters.Middleware("login", true, clientIPKey, 1, 1))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 3; i++ {
		if code := hit(t, r, "203.0.113.99:1234"); code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200 (fail open)", i+1, code)
		}
	}
	if got := testutil.CounterValueFor(t, "ratelimit_store_errors_total", "bucket", "login") - before; got < 3 {
		t.Errorf("store error counter increased by %v, want at least 3", got)
	}
}

// TestLimitersRunSkipsNonLocalStores 保证 Redis 后端不会让 cmd/server 多起一个
// 只会空转的清扫 goroutine（Redis 自己用 TTL 回收空桶）。
func TestLimitersRunSkipsNonLocalStores(t *testing.T) {
	t.Parallel()

	limiters := NewLimiters(WithRateLimitStore(failingStore{}))
	limiters.Middleware("login", true, clientIPKey, 1, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- limiters.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run blocked for a store without local state")
	}
}
