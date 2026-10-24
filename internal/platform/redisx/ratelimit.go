package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitKeyPrefix namespaces every counter this package writes, so a Redis
// instance shared with other applications (or with a future cache) never
// collides.
const rateLimitKeyPrefix = "ratelimit:"

// tokenBucketScript implements a token bucket atomically.
//
// Why Lua: the read-modify-write of (tokens, timestamp) has to be atomic across
// replicas, otherwise two requests can both see "one token left" and both pass.
// Redis executes a script atomically, so N replicas behave like one limiter.
//
// Why TIME: the server clock is the only one all replicas agree on; using each
// app process's wall clock would let a skewed replica refill (or starve) every
// other replica's buckets.
//
// KEYS[1] bucket key; ARGV[1] rps; ARGV[2] burst; ARGV[3] ttl in milliseconds.
var tokenBucketScript = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) + tonumber(t[2]) / 1000000
local rps = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])

local state = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(state[1])
local ts = tonumber(state[2])
if tokens == nil or ts == nil then
  tokens = burst
  ts = now
end

local elapsed = now - ts
if elapsed < 0 then elapsed = 0 end
tokens = math.min(burst, tokens + elapsed * rps)
if tokens > burst then tokens = burst end

local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end

redis.call('HSET', KEYS[1], 'tokens', string.format('%.6f', tokens), 'ts', string.format('%.6f', now))
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return allowed
`)

// RateLimitStore is a Redis-backed token bucket shared by every replica.
//
// It satisfies web.RateLimitStore by structure (this package must stay a leaf,
// so it cannot import the transport layer).
type RateLimitStore struct {
	client redis.UniversalClient
}

// NewRateLimitStore wraps an existing client.
func NewRateLimitStore(client redis.UniversalClient) *RateLimitStore {
	return &RateLimitStore{client: client}
}

// Allow consumes one token for key in bucket and reports whether the caller may
// proceed. rps and burst must be positive (config validation enforces it); a
// non-positive value means "no limit" and always allows.
func (s *RateLimitStore) Allow(ctx context.Context, bucket, key string, rps float64, burst int) (bool, error) {
	if rps <= 0 || burst <= 0 {
		return true, nil
	}
	allowed, err := tokenBucketScript.Run(ctx, s.client,
		[]string{rateLimitKey(bucket, key)},
		rps, burst, bucketTTL(rps, burst).Milliseconds(),
	).Int64()
	if err != nil {
		return false, fmt.Errorf("redis token bucket: %w", err)
	}
	return allowed == 1, nil
}

func rateLimitKey(bucket, key string) string {
	return rateLimitKeyPrefix + bucket + ":" + key
}

// bucketTTL is how long an untouched bucket survives. A bucket refills to full
// in burst/rps seconds; keeping it for twice that (bounded to [1m, 1h]) wastes
// nothing on hot keys and lets Redis evict idle ones without the app sweeping.
func bucketTTL(rps float64, burst int) time.Duration {
	refill := time.Duration(float64(burst) / rps * float64(time.Second))
	ttl := 2 * refill
	if ttl < time.Minute {
		ttl = time.Minute
	}
	if ttl > time.Hour {
		ttl = time.Hour
	}
	// A non-finite rps (NaN/Inf) would overflow the conversion above; the config
	// layer rejects it, but this bounds the damage if a caller slips through.
	if ttl <= 0 || ttl > time.Hour {
		return time.Minute
	}
	return ttl
}
