package auth

import (
	"context"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/user"
)

// countingUsers records how many times VerifyAccess had to read the user row.
type countingUsers struct {
	*fakeUsers
	findByID int
}

func (c *countingUsers) FindByID(ctx context.Context, id uint64) (*user.User, error) {
	c.findByID++
	return c.fakeUsers.FindByID(ctx, id)
}

// enableCache turns the harness cache on with a deterministic clock.
func (h *harness) enableCache(ttl time.Duration) *countingUsers {
	counted := &countingUsers{fakeUsers: h.users}
	h.svc.users = counted
	h.svc.verifyCache = newVerifyCache(ttl)
	if h.svc.verifyCache != nil {
		h.svc.verifyCache.now = func() time.Time { return h.now }
	}
	return counted
}

func (h *harness) loginAccessToken(t *testing.T, email string) string {
	t.Helper()
	session, err := h.svc.Login(context.Background(), LoginInput{
		Email: email, Password: testPassword, ClientMeta: ClientMeta{UserAgent: "test", IP: "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return session.AccessToken
}

// TestVerifyAccessCachesUserRow pins the point of the cache: repeated requests
// with the same token read the row once.
func TestVerifyAccessCachesUserRow(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "cache@example.com", authkit.RoleMember, user.StatusActive)
	users := h.enableCache(5 * time.Second)
	token := h.loginAccessToken(t, u.Email)

	for i := 0; i < 3; i++ {
		if _, err := h.svc.VerifyAccess(context.Background(), token); err != nil {
			t.Fatalf("VerifyAccess #%d: %v", i, err)
		}
	}
	if users.findByID != 1 {
		t.Errorf("user row read %d times within the TTL, want 1", users.findByID)
	}
}

func TestVerifyAccessCacheExpires(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "ttl@example.com", authkit.RoleMember, user.StatusActive)
	users := h.enableCache(5 * time.Second)
	token := h.loginAccessToken(t, u.Email)

	if _, err := h.svc.VerifyAccess(context.Background(), token); err != nil {
		t.Fatalf("VerifyAccess: %v", err)
	}
	h.now = h.now.Add(5 * time.Second) // exactly at the deadline: stale
	if _, err := h.svc.VerifyAccess(context.Background(), token); err != nil {
		t.Fatalf("VerifyAccess after TTL: %v", err)
	}
	if users.findByID != 2 {
		t.Errorf("user row read %d times, want 2 (one per TTL window)", users.findByID)
	}
}

// TestVerifyAccessInvalidationRevokesImmediately is the property that makes the
// cache safe: a write in this process is visible on the very next request, not
// after the TTL.
func TestVerifyAccessInvalidationRevokesImmediately(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "revoke@example.com", authkit.RoleMember, user.StatusActive)
	users := h.enableCache(time.Hour)
	token := h.loginAccessToken(t, u.Email)

	if _, err := h.svc.VerifyAccess(context.Background(), token); err != nil {
		t.Fatalf("VerifyAccess: %v", err)
	}

	// Simulate a disable made through the user service: the row changes and the
	// invalidator is called.
	disabled := *u
	disabled.Status = user.StatusDisabled
	disabled.TokenVersion++
	h.users.users[u.ID] = &disabled
	h.svc.InvalidateAuthState(u.ID)

	if _, err := h.svc.VerifyAccess(context.Background(), token); err == nil {
		t.Fatal("cached entry survived invalidation: a disabled user still authenticated")
	}
	if users.findByID != 2 {
		t.Errorf("user row read %d times, want 2 (cache must be bypassed)", users.findByID)
	}
}

// TestVerifyAccessWithoutCacheAlwaysReads pins the disabled path: ttl=0 keeps
// the old behavior, one read per request.
func TestVerifyAccessWithoutCacheAlwaysReads(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "nocache@example.com", authkit.RoleMember, user.StatusActive)
	users := h.enableCache(0)
	if h.svc.verifyCache != nil {
		t.Fatal("ttl=0 must disable the cache")
	}
	token := h.loginAccessToken(t, u.Email)

	for i := 0; i < 3; i++ {
		if _, err := h.svc.VerifyAccess(context.Background(), token); err != nil {
			t.Fatalf("VerifyAccess #%d: %v", i, err)
		}
	}
	if users.findByID != 3 {
		t.Errorf("user row read %d times, want 3", users.findByID)
	}
}

// TestVerifyCacheIsBounded makes sure a scan of many ids cannot grow the map
// without limit.
func TestVerifyCacheIsBounded(t *testing.T) {
	cache := newVerifyCache(time.Minute)
	for i := uint64(1); i <= maxVerifyCacheEntries; i++ {
		cache.put(i, authState{role: authkit.RoleMember, active: true})
	}
	if cache.size() != maxVerifyCacheEntries {
		t.Fatalf("size = %d, want %d", cache.size(), maxVerifyCacheEntries)
	}
	cache.put(maxVerifyCacheEntries+1, authState{role: authkit.RoleMember, active: true})
	if size := cache.size(); size > maxVerifyCacheEntries {
		t.Errorf("size = %d after overflow, want it bounded", size)
	}
}

// TestVerifyCacheNilIsSafe documents that the cache methods are nil-safe so the
// uncached path needs no branches at the call sites.
func TestVerifyCacheNilIsSafe(t *testing.T) {
	var cache *verifyCache
	cache.put(1, authState{})
	if _, ok := cache.get(1); ok {
		t.Error("nil cache returned a hit")
	}
	cache.invalidate(1)
	if size := cache.size(); size != 0 {
		t.Errorf("nil cache size = %d", size)
	}
}
