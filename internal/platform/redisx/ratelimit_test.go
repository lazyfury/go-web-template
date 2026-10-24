package redisx

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/example/go-web-template/internal/platform/config"
)

func newTestStore(t *testing.T, addr string) *RateLimitStore {
	t.Helper()
	return NewRateLimitStore(redis.NewClient(&redis.Options{Addr: addr}))
}

func TestRateLimitStoreSharesBucketsAcrossReplicas(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	ctx := context.Background()

	// Two stores = two replicas pointing at the same Redis.
	replicaA := newTestStore(t, server.Addr())
	replicaB := newTestStore(t, server.Addr())

	const (
		rps   = 0.001 // effectively no refill during the test
		burst = 2
	)
	for i := 0; i < burst; i++ {
		allowed, err := replicaA.Allow(ctx, "login", "203.0.113.7", rps, burst)
		if err != nil {
			t.Fatalf("replica A allow %d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("replica A request %d denied, want allowed (burst=%d)", i+1, burst)
		}
	}

	// The next request hits replica B: if the counter were per process, it would
	// get its own full bucket and the limit would be burst*replicas.
	allowed, err := replicaB.Allow(ctx, "login", "203.0.113.7", rps, burst)
	if err != nil {
		t.Fatalf("replica B: %v", err)
	}
	if allowed {
		t.Fatal("replica B was allowed after replica A exhausted the shared bucket")
	}
}

func TestRateLimitStoreRefillsOverTime(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	ctx := context.Background()
	store := newTestStore(t, server.Addr())

	// The script refills from Redis TIME (the only clock all replicas agree on),
	// and miniredis does not fast-forward TIME, so the refill is exercised with
	// real time: 20 tokens/s means one token every 50ms.
	const (
		rps   = 20
		burst = 1
	)
	if allowed, err := store.Allow(ctx, "login", "k", rps, burst); err != nil || !allowed {
		t.Fatalf("first allow = %v, %v; want true, nil", allowed, err)
	}
	if allowed, err := store.Allow(ctx, "login", "k", rps, burst); err != nil || allowed {
		t.Fatalf("second allow = %v, %v; want false, nil", allowed, err)
	}

	time.Sleep(120 * time.Millisecond)
	if allowed, err := store.Allow(ctx, "login", "k", rps, burst); err != nil || !allowed {
		t.Fatalf("allow after refill = %v, %v; want true, nil", allowed, err)
	}
}

func TestRateLimitStoreIsolatesBucketsAndKeys(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	ctx := context.Background()
	store := newTestStore(t, server.Addr())

	const (
		rps   = 0.001
		burst = 1
	)
	if allowed, _ := store.Allow(ctx, "login", "same-key", rps, burst); !allowed {
		t.Fatal("first login bucket request denied")
	}
	// Same key, different bucket: the password guard must not share the login
	// counter (otherwise changing a password consumes the login allowance).
	if allowed, _ := store.Allow(ctx, "password", "same-key", rps, burst); !allowed {
		t.Error("password bucket was drained by the login bucket")
	}
	// Different key in the same bucket: unrelated client.
	if allowed, _ := store.Allow(ctx, "login", "other-key", rps, burst); !allowed {
		t.Error("other key was drained by same-key")
	}
	if allowed, _ := store.Allow(ctx, "login", "same-key", rps, burst); allowed {
		t.Error("same-key got a second token")
	}
}

func TestRateLimitStoreSetsATTLYSoIdleBucketsExpire(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	ctx := context.Background()
	store := newTestStore(t, server.Addr())

	if _, err := store.Allow(ctx, "login", "1.2.3.4", 1, 1); err != nil {
		t.Fatalf("allow: %v", err)
	}
	ttl := server.TTL(rateLimitKey("login", "1.2.3.4"))
	if ttl <= 0 {
		t.Fatalf("bucket TTL = %v, want a positive expiry (Redis must evict idle buckets)", ttl)
	}
}

func TestRateLimitStorePropagatesErrors(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	store := newTestStore(t, server.Addr())
	server.Close()

	allowed, err := store.Allow(context.Background(), "login", "k", 1, 1)
	if err == nil {
		t.Fatal("Allow returned no error after Redis disappeared")
	}
	if allowed {
		t.Error("Allow reported allowed on an error; the caller decides fail-open behavior")
	}
}

func TestRateLimitStoreWithoutLimitAlwaysAllows(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	store := newTestStore(t, server.Addr())

	for _, rps := range []float64{0, -1} {
		allowed, err := store.Allow(context.Background(), "login", "k", rps, 0)
		if err != nil || !allowed {
			t.Errorf("Allow(rps=%v, burst=0) = %v, %v; want true, nil", rps, allowed, err)
		}
	}
}

func TestBucketTTLStaysBounded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		rps   float64
		burst int
		want  time.Duration
	}{
		{"very slow refill is capped at an hour", 0.0001, 1, time.Hour},
		{"fast refill floors at a minute", 1000, 10, time.Minute},
		{"two refill windows in between", 0.1, 10, 200 * time.Second},
	}
	for _, tc := range cases {
		if got := bucketTTL(tc.rps, tc.burst); got != tc.want {
			t.Errorf("%s: bucketTTL(%v, %d) = %v, want %v", tc.name, tc.rps, tc.burst, got, tc.want)
		}
	}
}

func TestOpenFailsFastWhenRedisIsUnreachable(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client, err := Open(&config.Redis{
		Addr:        server.Addr(),
		PoolSize:    2,
		DialTimeout: time.Second,
		PingTimeout: time.Second,
	}, nil)
	if err != nil {
		t.Fatalf("Open(healthy): %v", err)
	}
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("ping after Open: %v", err)
	}
	_ = client.Close()

	// A deployment that configured Redis but cannot reach it must fail at
	// startup, not fall back to per-process buckets silently.
	if _, err := Open(&config.Redis{
		Addr:        "127.0.0.1:1",
		PoolSize:    1,
		DialTimeout: 200 * time.Millisecond,
		PingTimeout: 200 * time.Millisecond,
	}, nil); err == nil {
		t.Fatal("Open(unreachable) returned no error")
	}
}
