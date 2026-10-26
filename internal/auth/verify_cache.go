package auth

import (
	"sync"
	"time"

	"github.com/example/go-web-template/internal/platform/authkit"
)

// maxVerifyCacheEntries bounds the cache. Keys are user ids, so the realistic
// size is the user table, but a pathological caller (or a bug) must not be able
// to grow it without limit.
const maxVerifyCacheEntries = 8192

// authState is the slice of the user row that VerifyAccess needs on every
// authenticated request.
type authState struct {
	role         authkit.Role
	tokenVersion int
	active       bool
}

type verifyEntry struct {
	state     authState
	expiresAt time.Time
}

// verifyCache memoizes authState per user for a short TTL.
//
// Without it every authenticated request pays for a user-row read just to learn
// three columns. The cache removes that read without giving up revocation:
// every in-process write path (password change, role change, disable, delete,
// seeding) invalidates the entry, so a single instance still revokes on the very
// next request. A second replica can lag by at most the TTL, which is what
// auth.verify_cache_ttl documents — it is a deliberate trade, not an oversight.
type verifyCache struct {
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[uint64]verifyEntry
}

// newVerifyCache returns nil for a non-positive TTL, which disables caching:
// every method below is nil-safe, so the call sites stay branch-free.
func newVerifyCache(ttl time.Duration) *verifyCache {
	if ttl <= 0 {
		return nil
	}
	return &verifyCache{ttl: ttl, now: time.Now, entries: make(map[uint64]verifyEntry)}
}

// get returns the cached state for a user, dropping it if it has expired.
func (c *verifyCache) get(userID uint64) (authState, bool) {
	if c == nil {
		return authState{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[userID]
	if !ok {
		return authState{}, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, userID)
		return authState{}, false
	}
	return entry.state, true
}

func (c *verifyCache) put(userID uint64, state authState) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxVerifyCacheEntries {
		// Blunt but bounded: dropping everything costs one DB read per active
		// user, which is exactly what the uncached path did.
		clear(c.entries)
	}
	c.entries[userID] = verifyEntry{state: state, expiresAt: c.now().Add(c.ttl)}
}

// invalidate forgets one user. It is called by every write path that changes
// status, role, password or existence.
func (c *verifyCache) invalidate(userID uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, userID)
}

// len reports the number of live entries (tests and metrics).
func (c *verifyCache) size() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
