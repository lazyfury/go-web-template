package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
	"github.com/example/go-web-template/internal/user"
)

// UserStore is the narrow view of user persistence that auth needs. It is
// satisfied by user.Repository; keeping it local avoids importing the whole
// user service and makes the auth tests trivial to fake.
type UserStore interface {
	FindByEmail(ctx context.Context, email string) (*user.User, error)
	FindByID(ctx context.Context, id uint64) (*user.User, error)
	Create(ctx context.Context, u *user.User) error
	SaveAuthState(ctx context.Context, u *user.User) error
}

// Options tunes the token lifetimes and lockout policy.
type Options struct {
	RefreshTTL time.Duration
	// RefreshMaxLifetime caps one whole session (from login), regardless of
	// how often it is rotated.
	RefreshMaxLifetime time.Duration
	// RefreshReuseInterval tolerates replay of a just-rotated token.
	RefreshReuseInterval time.Duration
	MaxLoginAttempts     int
	Lockout              time.Duration
	// VerifyCacheTTL memoizes the per-user state that VerifyAccess reads on
	// every authenticated request. Zero disables the cache (one DB read per
	// request); a positive value trades a bounded revocation lag on *other*
	// replicas for that read. In-process writes always invalidate immediately.
	VerifyCacheTTL time.Duration
}

// ClientMeta is request context stored with a refresh session for auditing.
type ClientMeta struct {
	UserAgent string
	IP        string
}

// LoginInput is a validated login attempt.
type LoginInput struct {
	Email    string
	Password string
	ClientMeta
}

// Session is the result of a successful login or refresh: a new access token,
// the (rotated) refresh token to store in the cookie and the user view.
type Session struct {
	AccessToken      string
	AccessExpiresIn  int64
	RefreshToken     string
	RefreshTokenID   uint64
	RefreshExpiresAt time.Time
	User             *user.User
}

// Service implements authentication and implements authkit.Verifier.
type Service struct {
	users         UserStore
	tokens        Repository
	issuer        *authkit.TokenIssuer
	hasher        *passwd.Hasher
	logger        *zap.Logger
	refreshTTL    time.Duration
	maxLifetime   time.Duration
	reuseInterval time.Duration
	maxAttempts   int
	lockout       time.Duration
	verifyCache   *verifyCache
	now           func() time.Time
}

// NewService wires the auth use-cases.
func NewService(
	users UserStore,
	tokens Repository,
	issuer *authkit.TokenIssuer,
	hasher *passwd.Hasher,
	options Options,
	logger *zap.Logger,
) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		users:         users,
		tokens:        tokens,
		issuer:        issuer,
		hasher:        hasher,
		logger:        logger,
		refreshTTL:    options.RefreshTTL,
		maxLifetime:   options.RefreshMaxLifetime,
		reuseInterval: options.RefreshReuseInterval,
		maxAttempts:   options.MaxLoginAttempts,
		lockout:       options.Lockout,
		verifyCache:   newVerifyCache(options.VerifyCacheTTL),
		now:           time.Now,
	}
}

// Login verifies credentials and starts a session.
//
// Failure modes are deliberately indistinguishable to the client (the same 401
// and message for unknown email, no credentials and wrong password), and the
// unknown-email path still runs a bcrypt comparison so timing does not leak
// account existence.
func (s *Service) Login(ctx context.Context, in LoginInput) (*Session, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))

	account, err := s.users.FindByEmail(ctx, email)
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		s.hasher.BurnTiming(in.Password)
		s.logger.Warn("login failed", zap.String("reason", "unknown_email"), zap.String("email", email))
		loginTotal.WithLabelValues("unknown_email").Inc()
		return nil, invalidCredentials()
	case err != nil:
		loginTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	// Locked and disabled accounts must answer exactly like a wrong password:
	// a distinct 403 (with the remaining lock time) told an anonymous caller
	// which addresses exist — five requests per candidate — and advertised the
	// lockout as a way to knock a known account offline. The reason stays in
	// the log, never in the response. BurnTiming keeps the cost identical too,
	// otherwise a locked account would answer measurably faster than an
	// unknown one and leak the same fact.
	if account.LockedUntil != nil && s.now().Before(*account.LockedUntil) {
		s.hasher.BurnTiming(in.Password)
		s.logger.Warn("login failed", zap.String("reason", "locked"), zap.Uint64("user_id", account.ID))
		loginTotal.WithLabelValues("locked").Inc()
		return nil, invalidCredentials()
	}
	if !account.IsActive() {
		s.hasher.BurnTiming(in.Password)
		s.logger.Warn("login failed", zap.String("reason", "disabled"), zap.Uint64("user_id", account.ID))
		loginTotal.WithLabelValues("disabled").Inc()
		return nil, invalidCredentials()
	}
	if !s.hasher.Verify(account.PasswordHash, in.Password) {
		s.recordFailure(ctx, account)
		return nil, invalidCredentials()
	}

	now := s.now()
	account.FailedLoginCount = 0
	account.LockedUntil = nil
	account.LastLoginAt = &now
	if err := s.users.SaveAuthState(ctx, account); err != nil {
		return nil, err
	}
	s.logger.Info("login succeeded", zap.Uint64("user_id", account.ID), zap.String("role", string(account.Role)))
	loginTotal.WithLabelValues("success").Inc()
	return s.startSession(ctx, account, in.ClientMeta, now)
}

// Refresh rotates a refresh token: the presented token is revoked and a new
// one is issued. Two security controls sit around that rotation:
//
//   - reuse interval: a token rotated away less than RefreshReuseInterval ago
//     is tolerated (several tabs sharing one cookie jar race on startup). The
//     replay still gets a fresh token, but every sibling token is revoked so
//     the number of live tokens stays bounded.
//   - absolute session cap: SessionStartedAt is carried across rotations, so a
//     session cannot outlive RefreshMaxLifetime even if it is used constantly.
//
// Outside the reuse window an already-revoked token is treated as theft and
// every session of that user is revoked.
func (s *Service) Refresh(ctx context.Context, rawToken string, meta ClientMeta) (*Session, error) {
	if rawToken == "" {
		refreshTotal.WithLabelValues("invalid").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "missing refresh token")
	}
	stored, err := s.tokens.FindByHash(ctx, hashToken(rawToken))
	if errors.Is(err, apperr.ErrNotFound) {
		refreshTotal.WithLabelValues("invalid").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid refresh token")
	}
	if err != nil {
		refreshTotal.WithLabelValues("error").Inc()
		return nil, err
	}

	now := s.now()
	if !now.Before(stored.ExpiresAt) {
		refreshTotal.WithLabelValues("expired").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "refresh token expired")
	}
	if s.sessionExpired(stored, now) {
		if err := s.revokeAllSessions(ctx, stored.UserID, now, RevokeSessionCap); err != nil {
			return nil, err
		}
		s.logger.Info("session reached its absolute lifetime", zap.Uint64("user_id", stored.UserID))
		refreshTotal.WithLabelValues("session_cap").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "session expired, please sign in again")
	}

	account, err := s.users.FindByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			refreshTotal.WithLabelValues("invalid").Inc()
			return nil, apperr.New(apperr.CodeUnauthorized, "invalid refresh token")
		}
		refreshTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	if !account.IsActive() {
		refreshTotal.WithLabelValues("disabled").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "account disabled")
	}

	if stored.RevokedAt != nil {
		if s.withinReuseInterval(stored, now) {
			return s.toleratedReplay(ctx, account, meta, stored, now)
		}
		if err := s.revokeAllSessions(ctx, stored.UserID, now, RevokeReuse); err != nil {
			return nil, err
		}
		s.logger.Error("refresh token reuse detected; all sessions revoked", zap.Uint64("user_id", stored.UserID))
		refreshTotal.WithLabelValues("reuse").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid refresh token")
	}

	revoked, err := s.tokens.RevokeIfActive(ctx, stored.ID, now, RevokeRotated)
	if err != nil {
		return nil, err
	}
	if !revoked {
		// Lost the race against a concurrent rotate. Re-read the row: if it was
		// just revoked we are inside the reuse window and can continue.
		reloaded, findErr := s.tokens.FindByHash(ctx, hashToken(rawToken))
		if findErr == nil && reloaded.RevokedAt != nil && s.withinReuseInterval(reloaded, now) {
			return s.toleratedReplay(ctx, account, meta, reloaded, now)
		}
		if err := s.revokeAllSessions(ctx, stored.UserID, now, RevokeReuse); err != nil {
			return nil, err
		}
		s.logger.Error("concurrent refresh token rotation; all sessions revoked", zap.Uint64("user_id", stored.UserID))
		refreshTotal.WithLabelValues("reuse").Inc()
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid refresh token")
	}
	session, err := s.startSession(ctx, account, meta, stored.SessionStartedAt)
	if err != nil {
		return nil, err
	}
	refreshTotal.WithLabelValues("ok").Inc()
	return session, nil
}

// toleratedReplay issues a fresh session for a token replayed inside the reuse
// window and revokes sibling tokens, keeping exactly one live refresh token.
func (s *Service) toleratedReplay(ctx context.Context, account *user.User, meta ClientMeta, stored *RefreshToken, now time.Time) (*Session, error) {
	refreshTotal.WithLabelValues("tolerated_replay").Inc()
	session, err := s.startSession(ctx, account, meta, stored.SessionStartedAt)
	if err != nil {
		return nil, err
	}
	if err := s.tokens.RevokeOthersForUser(ctx, account.ID, session.RefreshTokenID, now, RevokeSuperseded); err != nil {
		return nil, err
	}
	s.logger.Warn("refresh token replay tolerated inside the reuse interval",
		zap.Uint64("user_id", account.ID), zap.Duration("reuse_interval", s.reuseInterval))
	return session, nil
}

// withinReuseInterval only tolerates tokens revoked by a rotation. Logout,
// password changes, reuse detection and session-cap revocation are immediate:
// otherwise a just-logged-out token could resurrect the session.
func (s *Service) withinReuseInterval(token *RefreshToken, now time.Time) bool {
	if token.RevokedAt == nil || token.RevokedReason != RevokeRotated || s.reuseInterval <= 0 {
		return false
	}
	return now.Sub(*token.RevokedAt) <= s.reuseInterval
}

func (s *Service) sessionExpired(token *RefreshToken, now time.Time) bool {
	if s.maxLifetime <= 0 {
		return false
	}
	started := token.SessionStartedAt
	if started.IsZero() {
		started = token.CreatedAt
	}
	return !now.Before(started.Add(s.maxLifetime))
}

// Logout revokes the presented refresh token. It is idempotent.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	stored, err := s.tokens.FindByHash(ctx, hashToken(rawToken))
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.tokens.RevokeIfActive(ctx, stored.ID, s.now(), RevokeLogout); err != nil {
		return err
	}
	// Logging out means "end every session of this account", not just the
	// browser that asked (AGENTS.md §2.11 / design-auth §2). Revoking only the
	// presented token left a stolen session alive after the user pressed
	// logout, which is exactly what the user was trying to stop. The reason is
	// RevokeLogout (not RevokeRotated), so the reuse grace window never
	// resurrects any of them.
	if err := s.revokeAllSessions(ctx, stored.UserID, s.now(), RevokeLogout); err != nil {
		return err
	}
	return nil
}

// VerifyAccess implements authkit.Verifier: it parses the JWT and then checks
// the live user row, so disabled accounts and password changes take effect
// immediately instead of waiting for the access token to expire.
//
// The row is read through a short-TTL cache (auth.verify_cache_ttl): every write
// path in this process invalidates the entry, so "immediately" still holds for
// a single instance; another replica can lag by at most the TTL.
func (s *Service) VerifyAccess(ctx context.Context, rawToken string) (*authkit.Principal, error) {
	principal, err := s.issuer.Parse(rawToken)
	if err != nil {
		return nil, err
	}
	state, ok := s.verifyCache.get(principal.UserID)
	if !ok {
		account, err := s.users.FindByID(ctx, principal.UserID)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				return nil, apperr.New(apperr.CodeUnauthorized, "invalid access token")
			}
			return nil, err
		}
		state = authState{
			role:         account.Role,
			tokenVersion: account.TokenVersion,
			active:       account.IsActive(),
		}
		s.verifyCache.put(principal.UserID, state)
	}
	if !state.active || state.tokenVersion != principal.TokenVersion {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid access token")
	}
	if !state.role.Valid() {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid access token")
	}
	// The database is authoritative for the role, so a role change does not
	// require waiting for the token to expire.
	principal.Role = state.role
	return principal, nil
}

// InvalidateAuthState drops the cached authorization state of one user, so a
// change made through another service (role, status, password, delete) is
// visible on the next request instead of after verify_cache_ttl. The composition
// root wires it into the user service.
func (s *Service) InvalidateAuthState(userID uint64) {
	s.verifyCache.invalidate(userID)
}

// Me returns the authenticated user.
func (s *Service) Me(ctx context.Context, userID uint64) (*user.User, error) {
	return s.users.FindByID(ctx, userID)
}

// ChangePassword verifies the current password, rotates the hash, bumps the
// token version (invalidating every access token) and revokes all sessions.
func (s *Service) ChangePassword(ctx context.Context, userID uint64, currentPassword, newPassword string) error {
	account, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if !s.hasher.Verify(account.PasswordHash, currentPassword) {
		return apperr.New(apperr.CodeUnauthorized, "current password is incorrect")
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return apperr.Wrap(apperr.CodeInvalidArgument, err.Error(), err)
	}
	now := s.now()
	account.PasswordHash = hash
	account.PasswordChangedAt = &now
	account.TokenVersion++
	if err := s.users.SaveAuthState(ctx, account); err != nil {
		return err
	}
	// The bump above must be visible on the next request, not after the TTL.
	s.verifyCache.invalidate(account.ID)
	if err := s.revokeAllSessions(ctx, account.ID, now, RevokePassword); err != nil {
		return err
	}
	s.logger.Info("password changed", zap.Uint64("user_id", account.ID))
	return nil
}

// SeedAdmin provisions (or resets) an administrator account. It backs the
// cmd/admin CLI so a fresh deployment can log in without a public signup flow.
func (s *Service) SeedAdmin(ctx context.Context, name, email, password string, role authkit.Role) (*user.User, error) {
	if !role.Valid() {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, "invalid role", nil)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, err.Error(), err)
	}
	now := s.now()

	account, err := s.users.FindByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		account = &user.User{
			Name:              strings.TrimSpace(name),
			Email:             strings.ToLower(strings.TrimSpace(email)),
			Status:            user.StatusActive,
			Role:              role,
			PasswordHash:      hash,
			PasswordChangedAt: &now,
		}
		if err := s.users.Create(ctx, account); err != nil {
			return nil, err
		}
		return account, nil
	case err != nil:
		return nil, err
	}

	account.Name = strings.TrimSpace(name)
	account.Status = user.StatusActive
	account.Role = role
	account.PasswordHash = hash
	account.PasswordChangedAt = &now
	account.TokenVersion++
	account.FailedLoginCount = 0
	account.LockedUntil = nil
	if err := s.users.SaveAuthState(ctx, account); err != nil {
		return nil, err
	}
	// A credential reset must invalidate previous sessions and any cached state.
	s.verifyCache.invalidate(account.ID)
	if err := s.revokeAllSessions(ctx, account.ID, now, RevokePassword); err != nil {
		return nil, err
	}
	return account, nil
}

// revokeAllSessions is the hard revocation used by logout, password changes,
// reuse detection and the session cap: every active token dies, and every
// already-rotated token stops being tolerated by the reuse grace window.
// Without the second step a token rotated seconds before the logout could be
// replayed afterwards and mint a brand new session.
func (s *Service) revokeAllSessions(ctx context.Context, userID uint64, now time.Time, reason string) error {
	if err := s.tokens.RevokeAllForUser(ctx, userID, now, reason); err != nil {
		return err
	}
	return s.tokens.RetireRotatedForUser(ctx, userID, reason)
}

// recordFailure increments the failed-login counter and locks the account once
// the threshold is reached.
func (s *Service) recordFailure(ctx context.Context, account *user.User) {
	account.FailedLoginCount++
	if s.maxAttempts > 0 && int(account.FailedLoginCount) >= s.maxAttempts {
		until := s.now().Add(s.lockout)
		account.LockedUntil = &until
		account.FailedLoginCount = 0
		s.logger.Warn("login failed", zap.String("reason", "locked_out"), zap.Uint64("user_id", account.ID))
		loginTotal.WithLabelValues("locked_out").Inc()
		accountLockoutsTotal.Inc()
	} else {
		s.logger.Warn("login failed", zap.String("reason", "bad_password"), zap.Uint64("user_id", account.ID))
		loginTotal.WithLabelValues("bad_password").Inc()
	}
	if err := s.users.SaveAuthState(ctx, account); err != nil {
		s.logger.Error("persist failed-login state", zap.Uint64("user_id", account.ID), zap.Error(err))
	}
}

// startSession issues an access token and persists a refresh session.
// sessionStartedAt is carried over on rotation so the absolute cap holds.
func (s *Service) startSession(ctx context.Context, account *user.User, meta ClientMeta, sessionStartedAt time.Time) (*Session, error) {
	accessToken, err := s.issuer.Issue(account.ID, account.Role, account.TokenVersion)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "issue access token failed", err)
	}
	rawRefresh, err := newRefreshToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "generate refresh token failed", err)
	}
	now := s.now()
	if sessionStartedAt.IsZero() {
		sessionStartedAt = now
	}
	expiresAt := now.Add(s.refreshTTL)
	if s.maxLifetime > 0 {
		// Never slide past the absolute session cap.
		if deadline := sessionStartedAt.Add(s.maxLifetime); deadline.Before(expiresAt) {
			expiresAt = deadline
		}
	}
	stored := &RefreshToken{
		UserID:           account.ID,
		TokenHash:        hashToken(rawRefresh),
		ExpiresAt:        expiresAt,
		SessionStartedAt: sessionStartedAt,
		UserAgent:        truncate(meta.UserAgent, 255),
		IP:               truncate(meta.IP, 64),
		CreatedAt:        now,
	}
	if err := s.tokens.Create(ctx, stored); err != nil {
		return nil, err
	}
	return &Session{
		AccessToken:      accessToken,
		AccessExpiresIn:  int64(s.issuer.TTL().Seconds()),
		RefreshToken:     rawRefresh,
		RefreshTokenID:   stored.ID,
		RefreshExpiresAt: expiresAt,
		User:             account,
	}, nil
}

func newRefreshToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken returns the hex SHA-256 digest stored in the database.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func invalidCredentials() error {
	return apperr.New(apperr.CodeUnauthorized, "invalid credentials")
}
