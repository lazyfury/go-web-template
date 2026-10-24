package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
	"github.com/example/go-web-template/internal/user"
)

const (
	testSecret   = "auth-test-secret-that-is-32-bytes!!"
	testPassword = "correct-horse-battery"
)

type fakeUsers struct {
	users  map[uint64]*user.User
	nextID uint64
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{users: make(map[uint64]*user.User), nextID: 1}
}

func (f *fakeUsers) FindByEmail(_ context.Context, email string) (*user.User, error) {
	for _, u := range f.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (f *fakeUsers) FindByID(_ context.Context, id uint64) (*user.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, apperr.ErrNotFound
}

func (f *fakeUsers) Create(_ context.Context, u *user.User) error {
	u.ID = f.nextID
	f.nextID++
	f.users[u.ID] = u
	return nil
}

func (f *fakeUsers) SaveAuthState(_ context.Context, u *user.User) error {
	f.users[u.ID] = u
	return nil
}

type fakeTokens struct {
	tokens map[string]*RefreshToken
	nextID uint64
	// now mirrors the service clock so grace-window comparisons are deterministic.
	now func() time.Time
}

func newFakeTokens() *fakeTokens {
	return &fakeTokens{tokens: make(map[string]*RefreshToken), nextID: 1, now: time.Now}
}

func (f *fakeTokens) Create(_ context.Context, token *RefreshToken) error {
	token.ID = f.nextID
	f.nextID++
	cp := *token
	f.tokens[token.TokenHash] = &cp
	return nil
}

func (f *fakeTokens) FindByHash(_ context.Context, hash string) (*RefreshToken, error) {
	stored, ok := f.tokens[hash]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	cp := *stored
	return &cp, nil
}

func (f *fakeTokens) RevokeIfActive(_ context.Context, id uint64, revokedAt time.Time, reason string) (bool, error) {
	for _, token := range f.tokens {
		if token.ID != id || token.RevokedAt != nil {
			continue
		}
		at := revokedAt
		token.RevokedAt = &at
		token.RevokedReason = reason
		return true, nil
	}
	return false, nil
}

func (f *fakeTokens) RevokeOthersForUser(_ context.Context, userID, keepID uint64, revokedAt time.Time, reason string) error {
	for _, token := range f.tokens {
		if token.UserID == userID && token.ID != keepID && token.RevokedAt == nil {
			at := revokedAt
			token.RevokedAt = &at
			token.RevokedReason = reason
		}
	}
	return nil
}

func (f *fakeTokens) RevokeAllForUser(_ context.Context, userID uint64, revokedAt time.Time, reason string) error {
	for _, token := range f.tokens {
		if token.UserID == userID && token.RevokedAt == nil {
			at := revokedAt
			token.RevokedAt = &at
			token.RevokedReason = reason
		}
	}
	return nil
}

func (f *fakeTokens) activeCount() int {
	count := 0
	for _, token := range f.tokens {
		if token.RevokedAt == nil {
			count++
		}
	}
	return count
}

type harness struct {
	svc    *Service
	users  *fakeUsers
	tokens *fakeTokens
	now    time.Time
	issuer *authkit.TokenIssuer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	users := newFakeUsers()
	tokens := newFakeTokens()
	issuer := authkit.NewTokenIssuer(testSecret, "go-web-template-test", 15*time.Minute)
	svc := NewService(users, tokens, issuer, passwd.NewHasher(4), Options{
		RefreshTTL:           30 * 24 * time.Hour,
		RefreshMaxLifetime:   90 * 24 * time.Hour,
		RefreshReuseInterval: 30 * time.Second,
		MaxLoginAttempts:     3,
		Lockout:              15 * time.Minute,
	}, zap.NewNop())
	h := &harness{svc: svc, users: users, tokens: tokens, now: time.Unix(1_700_000_000, 0).UTC(), issuer: issuer}
	svc.now = func() time.Time { return h.now }
	tokens.now = func() time.Time { return h.now }
	return h
}

func (h *harness) seedUser(t *testing.T, email string, role authkit.Role, status user.Status) *user.User {
	t.Helper()
	hash, err := passwd.NewHasher(4).Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := &user.User{Name: "Test", Email: email, Role: role, Status: status, PasswordHash: hash}
	if err := h.users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func TestLoginSuccess(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "admin@example.com", authkit.RoleAdmin, user.StatusActive)

	session, err := h.svc.Login(context.Background(), LoginInput{
		Email: "Admin@Example.com", Password: testPassword, ClientMeta: ClientMeta{UserAgent: "test", IP: "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatal("session must carry both tokens")
	}
	if session.RefreshToken == hashToken(session.RefreshToken) {
		t.Error("refresh token must be stored hashed, not raw")
	}
	if len(h.tokens.tokens) != 1 {
		t.Errorf("stored refresh tokens = %d, want 1", len(h.tokens.tokens))
	}
	if u.LastLoginAt == nil {
		t.Error("last_login_at should be recorded")
	}
	principal, err := h.issuer.Parse(session.AccessToken)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	if principal.UserID != u.ID || principal.Role != authkit.RoleAdmin {
		t.Errorf("principal = %+v", principal)
	}
}

func TestLoginUnknownEmailAndWrongPasswordAreIndistinguishable(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)

	_, errUnknown := h.svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: testPassword})
	_, errWrong := h.svc.Login(context.Background(), LoginInput{Email: "member@example.com", Password: "wrong-password"})

	for _, err := range []error{errUnknown, errWrong} {
		if !errors.Is(err, apperr.ErrUnauthorized) {
			t.Fatalf("expected unauthorized, got %v", err)
		}
	}
	if errUnknown.Error() != errWrong.Error() {
		t.Errorf("messages differ: %q vs %q", errUnknown.Error(), errWrong.Error())
	}
	// The unknown-email attempt must not create or touch any account.
	if len(h.users.users) != 1 {
		t.Errorf("users = %d, want 1", len(h.users.users))
	}
}

func TestLoginLocksAfterMaxAttempts(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)

	for i := 0; i < 3; i++ {
		if _, err := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: "wrong-password"}); !errors.Is(err, apperr.ErrUnauthorized) {
			t.Fatalf("attempt %d: expected unauthorized, got %v", i+1, err)
		}
	}
	if u.LockedUntil == nil {
		t.Fatal("account should be locked after max attempts")
	}

	_, err := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: testPassword})
	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("locked account should be forbidden even with the right password, got %v", err)
	}

	// After the lockout window the correct password works again.
	h.now = h.now.Add(16 * time.Minute)
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: testPassword}); err != nil {
		t.Fatalf("login after lockout: %v", err)
	}
}

func TestLoginDisabledAccount(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "off@example.com", authkit.RoleMember, user.StatusDisabled)
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: testPassword}); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("disabled account should be forbidden, got %v", err)
	}
}

func TestRefreshRotatesWithinReuseInterval(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, err := h.svc.Login(context.Background(), LoginInput{Email: "member@example.com", Password: testPassword})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	rotated, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if rotated.RefreshToken == session.RefreshToken {
		t.Error("refresh must rotate the token")
	}

	// A second tab racing with the same cookie is tolerated inside the reuse
	// window: it gets a fresh session and only one token stays live.
	replayed, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{})
	if err != nil {
		t.Fatalf("replay inside the reuse interval should be tolerated: %v", err)
	}
	if replayed.RefreshToken == "" {
		t.Fatal("tolerated replay must return a rotated token")
	}
	if got := h.tokens.activeCount(); got != 1 {
		t.Errorf("active refresh tokens = %d, want exactly 1 after tolerated replay", got)
	}
	// The newest token keeps working.
	if _, err := h.svc.Refresh(context.Background(), replayed.RefreshToken, ClientMeta{}); err != nil {
		t.Errorf("newest token should refresh: %v", err)
	}
}

func TestRefreshReuseOutsideIntervalRevokesAll(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, _ := h.svc.Login(context.Background(), LoginInput{Email: "member@example.com", Password: testPassword})

	rotated, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Outside the reuse window the replay is treated as theft.
	h.now = h.now.Add(31 * time.Second)
	if _, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{}); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("reuse outside the window should be unauthorized, got %v", err)
	}
	if got := h.tokens.activeCount(); got != 0 {
		t.Errorf("active refresh tokens = %d, want 0 after reuse detection", got)
	}
	if _, err := h.svc.Refresh(context.Background(), rotated.RefreshToken, ClientMeta{}); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("the rotated token must be revoked too, got %v", err)
	}
}

func TestRefreshSlidesUntilAbsoluteCap(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, _ := h.svc.Login(context.Background(), LoginInput{Email: "member@example.com", Password: testPassword})
	started := session.RefreshExpiresAt.Add(-30 * 24 * time.Hour)

	// Rotating every 20 days keeps the session alive (sliding) ...
	h.now = h.now.Add(20 * 24 * time.Hour)
	rotated, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{})
	if err != nil {
		t.Fatalf("slide 1: %v", err)
	}
	h.now = h.now.Add(20 * 24 * time.Hour)
	rotated, err = h.svc.Refresh(context.Background(), rotated.RefreshToken, ClientMeta{})
	if err != nil {
		t.Fatalf("slide 2: %v", err)
	}

	// ... but the session cannot outlive the absolute cap (90 days here).
	h.now = h.now.Add(51 * 24 * time.Hour) // 91 days after login
	stored, err := h.tokens.FindByHash(context.Background(), hashToken(rotated.RefreshToken))
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !stored.SessionStartedAt.Equal(started.UTC().Truncate(time.Second)) {
		t.Errorf("session_started_at = %v, want %v (carried across rotations)", stored.SessionStartedAt, started)
	}
	if _, err := h.svc.Refresh(context.Background(), rotated.RefreshToken, ClientMeta{}); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("session past its absolute cap should be unauthorized, got %v", err)
	}
}

func TestRefreshRejectsExpiredAndUnknown(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, _ := h.svc.Login(context.Background(), LoginInput{Email: "member@example.com", Password: testPassword})

	if _, err := h.svc.Refresh(context.Background(), "not-a-token", ClientMeta{}); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("unknown token should be unauthorized, got %v", err)
	}

	// refresh_ttl is 30 days: just before the absolute cap the token is expired.
	h.now = h.now.Add(31 * 24 * time.Hour)
	if _, err := h.svc.Refresh(context.Background(), session.RefreshToken, ClientMeta{}); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("expired token should be unauthorized, got %v", err)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, _ := h.svc.Login(context.Background(), LoginInput{Email: "member@example.com", Password: testPassword})

	if err := h.svc.Logout(context.Background(), session.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if got := h.tokens.activeCount(); got != 0 {
		t.Errorf("active tokens = %d, want 0", got)
	}
	// Logout is idempotent and tolerates unknown/empty cookies.
	if err := h.svc.Logout(context.Background(), ""); err != nil {
		t.Errorf("empty logout should be a no-op, got %v", err)
	}
	if err := h.svc.Logout(context.Background(), "unknown"); err != nil {
		t.Errorf("unknown logout should be a no-op, got %v", err)
	}
}

func TestVerifyAccessChecksLiveUser(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, _ := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: testPassword})

	principal, err := h.svc.VerifyAccess(context.Background(), session.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess: %v", err)
	}
	if principal.UserID != u.ID || principal.Role != authkit.RoleMember {
		t.Errorf("principal = %+v", principal)
	}

	// Role changes come from the database, not the token.
	u.Role = authkit.RoleAdmin
	principal, err = h.svc.VerifyAccess(context.Background(), session.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess after role change: %v", err)
	}
	if principal.Role != authkit.RoleAdmin {
		t.Errorf("role = %q, want admin (database is authoritative)", principal.Role)
	}

	// Password change / disable bumps the version and invalidates the token.
	u.TokenVersion++
	if _, err := h.svc.VerifyAccess(context.Background(), session.AccessToken); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("stale token version should be unauthorized, got %v", err)
	}

	u.TokenVersion = principal.TokenVersion
	u.Status = user.StatusDisabled
	if _, err := h.svc.VerifyAccess(context.Background(), session.AccessToken); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("disabled account should be unauthorized, got %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "member@example.com", authkit.RoleMember, user.StatusActive)
	session, _ := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: testPassword})

	if err := h.svc.ChangePassword(context.Background(), u.ID, "wrong-current", "new-password-123"); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("wrong current password should be unauthorized, got %v", err)
	}

	if err := h.svc.ChangePassword(context.Background(), u.ID, testPassword, "new-password-123"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if u.TokenVersion != 1 {
		t.Errorf("token version = %d, want 1", u.TokenVersion)
	}
	if got := h.tokens.activeCount(); got != 0 {
		t.Errorf("active refresh tokens = %d, want 0 after password change", got)
	}
	if _, err := h.svc.VerifyAccess(context.Background(), session.AccessToken); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Errorf("old access token should be invalid, got %v", err)
	}
	if _, err := h.svc.Login(context.Background(), LoginInput{Email: u.Email, Password: "new-password-123"}); err != nil {
		t.Errorf("login with the new password: %v", err)
	}
}

func TestSeedAdminCreatesAndResets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	created, err := h.svc.SeedAdmin(ctx, "Admin", "Admin@Example.com", "initial-password", authkit.RoleAdmin)
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if created.Role != authkit.RoleAdmin || created.Email != "admin@example.com" {
		t.Errorf("seeded account = %+v", created)
	}

	if _, err := h.svc.Login(ctx, LoginInput{Email: created.Email, Password: "initial-password"}); err != nil {
		t.Fatalf("login as seeded admin: %v", err)
	}

	// Re-seeding resets the password and revokes existing sessions.
	reset, err := h.svc.SeedAdmin(ctx, "Admin", created.Email, "rotated-password", authkit.RoleAdmin)
	if err != nil {
		t.Fatalf("SeedAdmin reset: %v", err)
	}
	if reset.ID != created.ID {
		t.Errorf("re-seed should reuse the account, got id %d want %d", reset.ID, created.ID)
	}
	if h.tokens.activeCount() != 0 {
		t.Errorf("re-seed must revoke sessions, %d still active", h.tokens.activeCount())
	}
	if _, err := h.svc.Login(ctx, LoginInput{Email: created.Email, Password: "rotated-password"}); err != nil {
		t.Errorf("login with rotated password: %v", err)
	}
	if _, err := h.svc.SeedAdmin(ctx, "X", "x@example.com", strings.Repeat("x", 9), authkit.Role("root")); !errors.Is(err, apperr.ErrInvalidArgument) {
		t.Errorf("invalid role should be rejected, got %v", err)
	}
}
