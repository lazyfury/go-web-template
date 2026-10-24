package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/auth"
	"github.com/example/go-web-template/internal/dict"
	"github.com/example/go-web-template/internal/message"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/testutil"
	"github.com/example/go-web-template/internal/user"
)

const (
	adminEmail    = "admin@example.test"
	adminPassword = "admin-password"
	memberEmail   = "member@example.test"
	memberPass    = "member-password"
)

// testEnv exercises the real composition root against an in-memory SQLite
// database: router → middleware → handler → service → repository. Because it
// calls app.NewWithDB, a wiring mistake in app.go fails here too.
type testEnv struct {
	app     *app.App
	db      *gorm.DB
	cfg     *config.Config
	handler http.Handler
}

func newTestEnv(t *testing.T, swaggerEnabled bool) *testEnv {
	t.Helper()
	return newTestEnvWithConfig(t, func(cfg *config.Config) { cfg.Swagger.Enabled = swaggerEnabled })
}

// newTestEnvWithConfig assembles the real composition root and lets a test
// tweak the config before the router is built (e.g. disable /metrics).
func newTestEnvWithConfig(t *testing.T, mutate func(*config.Config)) *testEnv {
	t.Helper()
	db := testutil.NewSQLiteDB(t, &user.User{}, &auth.RefreshToken{},
		&dict.Group{}, &dict.Item{},
		&message.Message{}, &message.Recipient{}, &message.Delivery{}, &message.Template{})

	cfg := config.Defaults()
	cfg.RateLimit.Enabled = false
	cfg.Auth.JWTSecret = "integration-test-secret-32-bytes!"
	// cost 4 keeps the suite fast; production uses auth.bcrypt_cost (12).
	cfg.Auth.BcryptCost = 4
	cfg.Auth.CookieSecure = false
	cfg.Message.WebhookURLs = []string{"https://example.com/hook"}
	if mutate != nil {
		mutate(cfg)
	}

	application, err := app.NewWithDB(cfg, zap.NewNop(), db)
	if err != nil {
		t.Fatalf("assemble app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return &testEnv{app: application, db: db, cfg: cfg, handler: application.Handler()}
}

func newTestApplication(t *testing.T, swaggerEnabled bool) *app.App {
	t.Helper()
	return newTestEnv(t, swaggerEnabled).app
}

func (e *testEnv) seedAdmin(t *testing.T) {
	t.Helper()
	if _, err := app.SeedAdmin(e.cfg, zap.NewNop(), e.db, app.SeedOptions{
		Name:     "Admin",
		Email:    adminEmail,
		Password: adminPassword,
		Role:     authkit.RoleAdmin,
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
}

// session holds what a browser would keep: the access token in memory and the
// refresh token in an HttpOnly cookie.
type session struct {
	accessToken string
	refresh     *http.Cookie
	user        struct {
		ID          uint64               `json:"id"`
		Email       string               `json:"email"`
		Role        authkit.Role         `json:"role"`
		Permissions []authkit.Permission `json:"permissions"`
	}
}

func (e *testEnv) login(t *testing.T, email, password string, wantStatus int) *session {
	t.Helper()
	w := doJSON(t, e.handler, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	})
	if w.Code != wantStatus {
		t.Fatalf("login status = %d, want %d, body=%s", w.Code, wantStatus, w.Body.String())
	}
	if wantStatus != http.StatusOK {
		return nil
	}
	env := decodeEnvelope(t, w)
	var payload struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID          uint64               `json:"id"`
			Email       string               `json:"email"`
			Role        authkit.Role         `json:"role"`
			Permissions []authkit.Permission `json:"permissions"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		t.Fatalf("decode login payload: %v", err)
	}
	cookies := w.Result().Cookies()
	var refresh *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == auth.RefreshCookieName {
			refresh = cookie
		}
	}
	if refresh == nil {
		t.Fatal("login must set the refresh cookie")
	}
	if !refresh.HttpOnly {
		t.Error("refresh cookie must be HttpOnly")
	}
	return &session{accessToken: payload.AccessToken, refresh: refresh, user: payload.User}
}

func (e *testEnv) loginAdmin(t *testing.T) *session {
	t.Helper()
	e.seedAdmin(t)
	return e.login(t, adminEmail, adminPassword, http.StatusOK)
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONWith(t, h, method, path, body, "", nil)
}

func doJSONAuth(t *testing.T, h http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONWith(t, h, method, path, body, token, nil)
}

func doJSONWith(t *testing.T, h http.Handler, method, path string, body any, token string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, w.Body.String())
	}
	return env
}

func createUser(t *testing.T, h http.Handler, token, name, email string) (userID uint64, statusCode int) {
	t.Helper()
	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": name, "email": email, "password": "initial-password",
	}, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID uint64 `json:"id"`
	}
	env := decodeEnvelope(t, w)
	if err := json.Unmarshal(env.Data, &created); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	return created.ID, w.Code
}

func TestUserCRUD(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	// Create.
	id, _ := createUser(t, h, admin.accessToken, "Alice", "alice@example.com")
	if id == 0 {
		t.Fatal("created user id should not be zero")
	}

	// Duplicate email → 409 conflict.
	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": "Alice2", "email": "ALICE@example.com", "password": "initial-password",
	}, admin.accessToken)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate create status = %d, want 409", w.Code)
	}

	// Validation error → 400.
	w = doJSONAuth(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": "", "email": "not-an-email", "password": "short",
	}, admin.accessToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid create status = %d, want 400", w.Code)
	}

	// Get.
	w = doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", id), nil, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Errorf("get status = %d, body=%s", w.Code, w.Body.String())
	}

	// Update.
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", id), map[string]string{
		"name": "Alicia",
	}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Errorf("update status = %d, body=%s", w.Code, w.Body.String())
	}
	body := decodeEnvelope(t, w)
	var updated map[string]any
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated["name"] != "Alicia" {
		t.Errorf("updated name = %v, want Alicia", updated["name"])
	}

	// List with keyword.
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/users?keyword=alicia&page=1&page_size=10", nil, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Errorf("list status = %d", w.Code)
	}
	body = decodeEnvelope(t, w)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Errorf("list total = %d items=%d, want 1/1", page.Total, len(page.Items))
	}

	// Delete.
	w = doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", id), nil, admin.accessToken)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", w.Code)
	}

	// Get after delete → 404.
	w = doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", id), nil, admin.accessToken)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete status = %d, want 404", w.Code)
	}

	// Invalid id → 400.
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/users/abc", nil, admin.accessToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", w.Code)
	}
}

func TestUserPhoneRoundTrip(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/users", map[string]any{
		"name": "Phone User", "email": "phone@example.com", "password": "initial-password", "phone": "+10000000000",
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create with phone = %d, body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID    uint64 `json:"id"`
		Phone string `json:"phone"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if created.Phone != "+10000000000" {
		t.Errorf("created phone = %q", created.Phone)
	}

	w = doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", created.ID), nil, admin.accessToken)
	var fetched struct {
		Phone string `json:"phone"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &fetched); err != nil {
		t.Fatalf("decode fetched user: %v", err)
	}
	if fetched.Phone != "+10000000000" {
		t.Errorf("fetched phone = %q", fetched.Phone)
	}

	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.ID), map[string]any{
		"phone": "+20000000000",
	}, admin.accessToken)
	var updated struct {
		Phone string `json:"phone"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &updated); err != nil {
		t.Fatalf("decode updated user: %v", err)
	}
	if updated.Phone != "+20000000000" {
		t.Errorf("updated phone = %q", updated.Phone)
	}

	// Empty string clears the optional phone.
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.ID), map[string]any{
		"phone": "",
	}, admin.accessToken)
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &updated); err != nil {
		t.Fatalf("decode cleared user: %v", err)
	}
	if updated.Phone != "" {
		t.Errorf("cleared phone = %q, want empty", updated.Phone)
	}
}

func TestAnonymousRequestsAreRejected(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPost, "/api/v1/users"},
		{http.MethodGet, "/api/v1/users/1"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, "/api/v1/dicts/user.status"},
		{http.MethodGet, "/api/v1/dict-groups"},
		{http.MethodGet, "/api/v1/messages/inbox"},
	} {
		w := doJSON(t, h, tc.method, tc.path, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, w.Code)
			continue
		}
		body := decodeEnvelope(t, w)
		if body.Code != 40100 {
			t.Errorf("%s %s code = %d, want 40100", tc.method, tc.path, body.Code)
		}
	}
}

func TestMemberPermissions(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	memberID, _ := createUser(t, h, admin.accessToken, "Mia", memberEmail)
	otherID, _ := createUser(t, h, admin.accessToken, "Otto", "otto@example.com")

	// The seeded password is "initial-password"; rotate it to the test constant.
	w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
		map[string]string{"password": memberPass}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("set member password = %d, body=%s", w.Code, w.Body.String())
	}
	member := env.login(t, memberEmail, memberPass, http.StatusOK)

	if member.user.Role != authkit.RoleMember {
		t.Errorf("role = %q, want member", member.user.Role)
	}
	if !containsPermission(member.user.Permissions, authkit.PermUsersReadSelf) {
		t.Errorf("member permissions = %v, want users:read:self", member.user.Permissions)
	}

	// Listing and creating users is admin-only.
	if w := doJSONAuth(t, h, http.MethodGet, "/api/v1/users", nil, member.accessToken); w.Code != http.StatusForbidden {
		t.Errorf("member list = %d, want 403", w.Code)
	}
	w = doJSONAuth(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": "Nope", "email": "nope@example.com", "password": "initial-password",
	}, member.accessToken)
	if w.Code != http.StatusForbidden {
		t.Errorf("member create = %d, want 403", w.Code)
	}

	// Reading own profile is allowed through both endpoints.
	if w := doJSONAuth(t, h, http.MethodGet, "/api/v1/auth/me", nil, member.accessToken); w.Code != http.StatusOK {
		t.Errorf("member me = %d, body=%s", w.Code, w.Body.String())
	}
	if w := doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", memberID), nil, member.accessToken); w.Code != http.StatusOK {
		t.Errorf("member get self = %d, want 200", w.Code)
	}

	// Another user's row is off limits, as is deleting and self-escalation.
	if w := doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", otherID), nil, member.accessToken); w.Code != http.StatusForbidden {
		t.Errorf("member get other = %d, want 403", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", otherID),
		map[string]string{"name": "Hacked"}, member.accessToken); w.Code != http.StatusForbidden {
		t.Errorf("member update other = %d, want 403", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
		map[string]string{"role": "admin"}, member.accessToken); w.Code != http.StatusForbidden {
		t.Errorf("member self-escalation = %d, want 403", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", memberID), nil, member.accessToken); w.Code != http.StatusForbidden {
		t.Errorf("member delete = %d, want 403", w.Code)
	}

	// Admin cannot delete their own account either.
	if w := doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", admin.user.ID), nil, admin.accessToken); w.Code != http.StatusForbidden {
		t.Errorf("admin self-delete = %d, want 403", w.Code)
	}
}

func TestRefreshFlow(t *testing.T) {
	env := newTestEnv(t, false)
	sess := env.loginAdmin(t)

	w := doJSONWith(t, env.handler, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{sess.refresh})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh = %d, body=%s", w.Code, w.Body.String())
	}
	var rotated struct {
		AccessToken string `json:"access_token"`
	}
	body := decodeEnvelope(t, w)
	if err := json.Unmarshal(body.Data, &rotated); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if rotated.AccessToken == "" {
		t.Fatal("refresh must return a new access token")
	}

	// Reusing the rotated-away cookie right away is tolerated (reuse interval):
	// several tabs share one cookie jar and would otherwise log the user out.
	w = doJSONWith(t, env.handler, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{sess.refresh})
	if w.Code != http.StatusOK {
		t.Errorf("refresh inside the reuse interval = %d, want 200", w.Code)
	}

	// The new access token still works for /auth/me.
	w = doJSONAuth(t, env.handler, http.MethodGet, "/api/v1/auth/me", nil, rotated.AccessToken)
	if w.Code != http.StatusOK {
		t.Errorf("me with refreshed token = %d, want 200", w.Code)
	}
}

func TestRefreshTokenHeaderTransportForAPIClients(t *testing.T) {
	env := newTestEnv(t, false)

	// Login with the body transport: no cookie, refresh token in the body.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"`+adminEmail+`","password":"`+adminPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(auth.RefreshTransportHeader, "body")
	env.seedAdmin(t)
	w := httptest.NewRecorder()
	env.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login body transport = %d, body=%s", w.Code, w.Body.String())
	}
	var login struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	body := decodeEnvelope(t, w)
	if err := json.Unmarshal(body.Data, &login); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if login.RefreshToken == "" {
		t.Fatal("body transport must return refresh_token")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("body transport must not set the refresh cookie")
	}

	// Refresh with the header: rotated token comes back in the body too.
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/refresh", http.NoBody)
	req.Header.Set(auth.RefreshTokenHeader, login.RefreshToken)
	w = httptest.NewRecorder()
	env.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("header refresh = %d, body=%s", w.Code, w.Body.String())
	}
	var rotated struct {
		RefreshToken string `json:"refresh_token"`
	}
	body = decodeEnvelope(t, w)
	if err := json.Unmarshal(body.Data, &rotated); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if rotated.RefreshToken == "" || rotated.RefreshToken == login.RefreshToken {
		t.Error("header refresh must rotate and return a new refresh_token")
	}

	// Access token from the header flow works like any other.
	w = doJSONAuth(t, env.handler, http.MethodGet, "/api/v1/users", nil, login.AccessToken)
	if w.Code != http.StatusOK {
		t.Errorf("header-flow access token = %d, want 200", w.Code)
	}
}

func TestLogoutIsImmediateEvenInsideReuseInterval(t *testing.T) {
	env := newTestEnv(t, false)
	admin := env.loginAdmin(t)

	// Logout needs the cookie too: the refresh session is identified by it.
	if w := doJSONWith(t, env.handler, http.MethodPost, "/api/v1/auth/logout", nil, admin.accessToken,
		[]*http.Cookie{admin.refresh}); w.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, body=%s", w.Code, w.Body.String())
	}
	// The revoked cookie must not be resurrected by the reuse interval.
	w := doJSONWith(t, env.handler, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{admin.refresh})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout = %d, want 401 (logout must be immediate)", w.Code)
	}
}

func TestPasswordChangeInvalidatesTokens(t *testing.T) {
	env := newTestEnv(t, false)
	admin := env.loginAdmin(t)

	w := doJSONAuth(t, env.handler, http.MethodPost, "/api/v1/auth/logout", nil, admin.accessToken)
	if w.Code != http.StatusNoContent {
		t.Errorf("logout = %d, want 204", w.Code)
	}

	w = doJSONAuth(t, env.handler, http.MethodPatch, "/api/v1/auth/password", map[string]string{
		"current_password": adminPassword,
		"new_password":     "rotated-password",
	}, admin.accessToken)
	if w.Code != http.StatusNoContent {
		t.Fatalf("change password = %d, body=%s", w.Code, w.Body.String())
	}

	// The access token that changed the password is now invalid.
	w = doJSONAuth(t, env.handler, http.MethodGet, "/api/v1/users", nil, admin.accessToken)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("stale access token = %d, want 401", w.Code)
	}

	// The old password no longer works; the new one does.
	env.login(t, adminEmail, adminPassword, http.StatusUnauthorized)
	env.login(t, adminEmail, "rotated-password", http.StatusOK)
}

func TestLoginFailures(t *testing.T) {
	env := newTestEnv(t, false)
	env.seedAdmin(t)

	env.login(t, adminEmail, "wrong-password", http.StatusUnauthorized)
	env.login(t, "nobody@example.test", adminPassword, http.StatusUnauthorized)

	// Malformed payloads are 400, not 401.
	w := doJSON(t, env.handler, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "not-an-email"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid login payload = %d, want 400", w.Code)
	}
}

func TestHealthEndpoints(t *testing.T) {
	h := newTestEnv(t, false).handler

	w := doJSON(t, h, http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("healthz status = %d", w.Code)
	}

	w = doJSON(t, h, http.MethodGet, "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("readyz status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestRequestIDHeaderEchoed(t *testing.T) {
	h := newTestEnv(t, false).handler

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("X-Request-ID", "test-request-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "test-request-123" {
		t.Errorf("request id = %q, want test-request-123", got)
	}
}

func TestNoRoute(t *testing.T) {
	h := newTestEnv(t, false).handler
	w := doJSON(t, h, http.MethodGet, "/api/v1/nope", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("no-route status = %d, want 404", w.Code)
	}
}

func (e *testEnv) createMember(t *testing.T, admin *session, email, name, phone string) uint64 {
	t.Helper()
	id, _ := createUser(t, e.handler, admin.accessToken, name, email)
	payload := map[string]any{"password": memberPass}
	if phone != "" {
		payload["phone"] = phone
	}
	w := doJSONAuth(t, e.handler, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", id), payload, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("prepare member = %d, body=%s", w.Code, w.Body.String())
	}
	return id
}

func TestMessageInboxFlow(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)
	memberID := env.createMember(t, admin, memberEmail, "Mia", "+10000000000")
	member := env.login(t, memberEmail, memberPass, http.StatusOK)

	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/messages", map[string]any{
		"title":      "Welcome",
		"body":       "Hello from the admin",
		"type":       "system",
		"level":      "info",
		"channels":   []string{"inbox"},
		"recipients": map[string]any{"user_ids": []uint64{memberID}},
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("send message = %d, body=%s", w.Code, w.Body.String())
	}
	var sent struct {
		MessageID      uint64 `json:"message_id"`
		RecipientCount int    `json:"recipient_count"`
		DeliveryCount  int    `json:"delivery_count"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &sent); err != nil {
		t.Fatalf("decode send result: %v", err)
	}
	if sent.MessageID == 0 || sent.RecipientCount != 1 || sent.DeliveryCount != 0 {
		t.Fatalf("send result = %+v", sent)
	}

	// Sent list is admin-only and exposes read statistics.
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/sent", nil, admin.accessToken)
	var sentList struct {
		Items []struct {
			RecipientCount int64 `json:"recipient_count"`
			ReadCount      int64 `json:"read_count"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &sentList); err != nil {
		t.Fatalf("decode sent list: %v", err)
	}
	if sentList.Total != 1 || len(sentList.Items) != 1 || sentList.Items[0].RecipientCount != 1 || sentList.Items[0].ReadCount != 0 {
		t.Fatalf("sent list = %+v", sentList)
	}

	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/inbox", nil, member.accessToken)
	var inbox struct {
		Items []struct {
			ID     uint64 `json:"id"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			ReadAt any    `json:"read_at"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &inbox); err != nil {
		t.Fatalf("decode inbox: %v", err)
	}
	if inbox.Total != 1 || len(inbox.Items) != 1 || inbox.Items[0].Title != "Welcome" || inbox.Items[0].ReadAt != nil {
		t.Fatalf("inbox = %+v", inbox)
	}
	messageID := inbox.Items[0].ID

	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/unread-count", nil, member.accessToken)
	var unread struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &unread); err != nil || unread.Count != 1 {
		t.Fatalf("unread = %+v err=%v", unread, err)
	}

	if w := doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/messages/inbox/%d", messageID), nil, member.accessToken); w.Code != http.StatusOK {
		t.Errorf("get inbox message = %d, body=%s", w.Code, w.Body.String())
	}
	if w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/messages/inbox/%d/read", messageID), nil, member.accessToken); w.Code != http.StatusNoContent {
		t.Errorf("mark read = %d", w.Code)
	}
	if err := json.Unmarshal(decodeEnvelope(t, doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/unread-count", nil, member.accessToken)).Data, &unread); err != nil || unread.Count != 0 {
		t.Errorf("unread after read = %+v err=%v", unread, err)
	}
	if w := doJSONAuth(t, h, http.MethodPost, "/api/v1/messages/read-all", nil, member.accessToken); w.Code != http.StatusNoContent {
		t.Errorf("read all = %d", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/messages/inbox/%d", messageID), nil, member.accessToken); w.Code != http.StatusNoContent {
		t.Errorf("delete inbox = %d", w.Code)
	}
	if err := json.Unmarshal(decodeEnvelope(t, doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/inbox", nil, member.accessToken)).Data, &inbox); err != nil || inbox.Total != 0 {
		t.Errorf("inbox after delete = %+v err=%v", inbox, err)
	}

	// Members cannot send or manage templates.
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/v1/messages", map[string]any{"channels": []string{"inbox"}, "recipients": map[string]any{"all": true}}},
		{http.MethodGet, "/api/v1/message-templates", nil},
		{http.MethodGet, "/api/v1/message-deliveries", nil},
	} {
		if w := doJSONAuth(t, h, tc.method, tc.path, tc.body, member.accessToken); w.Code != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403", tc.method, tc.path, w.Code)
		}
	}
}

func TestMessageMultiChannelDeliveries(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)
	memberID := env.createMember(t, admin, memberEmail, "Mia", "+10000000000")
	member := env.login(t, memberEmail, memberPass, http.StatusOK)

	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/messages", map[string]any{
		"title":      "Multi channel",
		"body":       "hello",
		"channels":   []string{"inbox", "email", "sms", "webhook"},
		"recipients": map[string]any{"user_ids": []uint64{memberID}},
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("send multi-channel = %d, body=%s", w.Code, w.Body.String())
	}
	var sent struct {
		MessageID      uint64 `json:"message_id"`
		RecipientCount int    `json:"recipient_count"`
		DeliveryCount  int    `json:"delivery_count"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.RecipientCount != 1 || sent.DeliveryCount != 3 {
		t.Fatalf("multi-channel result = %+v, want 1 recipient / 3 deliveries", sent)
	}

	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/message-deliveries", nil, admin.accessToken)
	var deliveries struct {
		Items []struct {
			Channel     string `json:"channel"`
			Target      string `json:"target"`
			Status      int    `json:"status"`
			MaxAttempts int    `json:"max_attempts"`
			ProviderID  string `json:"provider_message_id"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &deliveries); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	if deliveries.Total != 3 || len(deliveries.Items) != 3 {
		t.Fatalf("deliveries = %+v", deliveries)
	}
	targets := map[string]string{}
	for _, delivery := range deliveries.Items {
		targets[delivery.Channel] = delivery.Target
		if delivery.Status != 0 || delivery.MaxAttempts != 5 {
			t.Errorf("pending delivery = %+v", delivery)
		}
	}
	if targets["email"] != "member@example.test" || targets["sms"] != "+10000000000" || targets["webhook"] != "https://example.com/hook" {
		t.Errorf("delivery targets = %+v", targets)
	}

	// The inbox channel still produced one inbox entry.
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/inbox", nil, member.accessToken)
	var inbox struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &inbox); err != nil || inbox.Total != 1 {
		t.Errorf("inbox total = %+v err=%v, want 1", inbox, err)
	}

	// A channel with no usable target is rejected instead of silently dropped.
	w = doJSONAuth(t, h, http.MethodPost, "/api/v1/messages", map[string]any{
		"title":      "No phone",
		"body":       "hello",
		"channels":   []string{"sms"},
		"recipients": map[string]any{"all": false, "roles": []string{"admin"}},
	}, admin.accessToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("sms without phone = %d, want 400", w.Code)
	}
}

func TestMessageTemplateFlow(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)
	memberID := env.createMember(t, admin, memberEmail, "Mia", "")
	member := env.login(t, memberEmail, memberPass, http.StatusOK)

	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/message-templates", map[string]any{
		"key":       "user.welcome",
		"channel":   "inbox",
		"locale":    "zh-CN",
		"subject":   "Hi {{.Name}}",
		"body":      "Welcome {{.Name}}",
		"variables": []string{"Name"},
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create template = %d, body=%s", w.Code, w.Body.String())
	}
	var template struct {
		ID        uint64   `json:"id"`
		Variables []string `json:"variables"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &template); err != nil {
		t.Fatal(err)
	}
	if template.ID == 0 || len(template.Variables) != 1 || template.Variables[0] != "Name" {
		t.Fatalf("created template = %+v", template)
	}

	if w := doJSONAuth(t, h, http.MethodPost, "/api/v1/message-templates", map[string]any{
		"key": "user.welcome", "channel": "inbox", "locale": "zh-CN", "body": "dup",
	}, admin.accessToken); w.Code != http.StatusConflict {
		t.Errorf("duplicate template = %d, want 409", w.Code)
	}

	// Send using the template and verify the rendered snapshot in the inbox.
	w = doJSONAuth(t, h, http.MethodPost, "/api/v1/messages", map[string]any{
		"channels":     []string{"inbox"},
		"recipients":   map[string]any{"user_ids": []uint64{memberID}},
		"template_key": "user.welcome",
		"locale":       "zh-CN",
		"data":         map[string]any{"Name": "Mia"},
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("send template = %d, body=%s", w.Code, w.Body.String())
	}
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/inbox", nil, member.accessToken)
	var inbox struct {
		Items []struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &inbox); err != nil || inbox.Total != 1 {
		t.Fatalf("inbox after template = %+v err=%v", inbox, err)
	}
	if inbox.Items[0].Title != "Hi Mia" || inbox.Items[0].Body != "Welcome Mia" {
		t.Errorf("rendered message = %+v", inbox.Items[0])
	}

	// Unknown template variables are rejected.
	w = doJSONAuth(t, h, http.MethodPost, "/api/v1/messages", map[string]any{
		"channels":     []string{"inbox"},
		"recipients":   map[string]any{"user_ids": []uint64{memberID}},
		"template_key": "user.welcome",
		"data":         map[string]any{"Other": "x"},
	}, admin.accessToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown template variable = %d, want 400", w.Code)
	}

	// Update and delete.
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/message-templates/%d", template.ID), map[string]any{
		"subject": "Hello {{.Name}}",
		"status":  0,
	}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Errorf("update template = %d, body=%s", w.Code, w.Body.String())
	}
	if w := doJSONAuth(t, h, http.MethodGet, "/api/v1/message-templates", nil, admin.accessToken); w.Code != http.StatusOK {
		t.Errorf("list templates = %d", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/message-templates/%d", template.ID), nil, admin.accessToken); w.Code != http.StatusNoContent {
		t.Errorf("delete template = %d", w.Code)
	}
}

func TestDictCRUD(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	// Anonymous access is rejected before the handler runs.
	if w := doJSON(t, h, http.MethodGet, "/api/v1/dicts/user.status", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous dict read = %d, want 401", w.Code)
	}

	// Create a group (= setting).
	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/dict-groups", map[string]any{
		"key": "user.status", "name": "User status", "description": "Account states",
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create group = %d, body=%s", w.Code, w.Body.String())
	}
	var group struct {
		ID      uint64      `json:"id"`
		Key     string      `json:"key"`
		Status  dict.Status `json:"status"`
		ItemCnt int64       `json:"item_count"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &group); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if group.ID == 0 || group.Key != "user.status" || group.Status != dict.StatusEnabled {
		t.Fatalf("created group = %+v", group)
	}

	// Group keys are unique.
	w = doJSONAuth(t, h, http.MethodPost, "/api/v1/dict-groups", map[string]any{
		"key": "user.status", "name": "Dup",
	}, admin.accessToken)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate group = %d, want 409", w.Code)
	}

	// Create an item in the dict array.
	w = doJSONAuth(t, h, http.MethodPost, fmt.Sprintf("/api/v1/dict-groups/%d/items", group.ID), map[string]any{
		"key": "1", "label": "Active", "value": "1", "sort": 10,
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create item = %d, body=%s", w.Code, w.Body.String())
	}
	var item struct {
		ID    uint64 `json:"id"`
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item.ID == 0 || item.Key != "1" {
		t.Fatalf("created item = %+v", item)
	}

	// Same key inside the same group conflicts.
	w = doJSONAuth(t, h, http.MethodPost, fmt.Sprintf("/api/v1/dict-groups/%d/items", group.ID), map[string]any{
		"key": "1", "label": "Dup",
	}, admin.accessToken)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate item = %d, want 409", w.Code)
	}

	// Consumer read returns the dict array.
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/user.status", nil, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("get dict values = %d, body=%s", w.Code, w.Body.String())
	}
	var values []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &values); err != nil {
		t.Fatalf("decode values: %v", err)
	}
	if len(values) != 1 || values[0].Key != "1" || values[0].Label != "Active" || values[0].Value != "1" {
		t.Fatalf("values = %+v", values)
	}

	// Update the item.
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/dict-items/%d", item.ID), map[string]any{
		"label": "Enabled", "remark": "login allowed",
	}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("update item = %d, body=%s", w.Code, w.Body.String())
	}

	// Disabled items disappear from the consumer array but stay visible to admin.
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/dict-items/%d", item.ID), map[string]any{
		"status": 0,
	}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("disable item = %d, body=%s", w.Code, w.Body.String())
	}
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/user.status", nil, admin.accessToken)
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &values); err != nil {
		t.Fatalf("decode values after disable: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("values after disable = %+v, want empty", values)
	}
	w = doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/dict-groups/%d/items?include_disabled=true", group.ID), nil, admin.accessToken)
	var adminItems []struct {
		ID     uint64      `json:"id"`
		Status dict.Status `json:"status"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &adminItems); err != nil {
		t.Fatalf("decode admin items: %v", err)
	}
	if len(adminItems) != 1 || adminItems[0].Status != dict.StatusDisabled {
		t.Errorf("admin items = %+v, want one disabled item", adminItems)
	}

	// Group detail reports the item count; disabling the group hides it.
	w = doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/dict-groups/%d", group.ID), nil, admin.accessToken)
	var detail struct {
		ItemCount int64 `json:"item_count"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &detail); err != nil {
		t.Fatalf("decode group detail: %v", err)
	}
	if detail.ItemCount != 1 {
		t.Errorf("item_count = %d, want 1", detail.ItemCount)
	}
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/dict-groups/%d", group.ID), map[string]any{
		"status": 0,
	}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("disable group = %d, body=%s", w.Code, w.Body.String())
	}
	if w := doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/user.status", nil, admin.accessToken); w.Code != http.StatusNotFound {
		t.Errorf("disabled group read = %d, want 404", w.Code)
	}

	// Delete item then group.
	if w := doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/dict-items/%d", item.ID), nil, admin.accessToken); w.Code != http.StatusNoContent {
		t.Errorf("delete item = %d, want 204", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/dict-groups/%d", group.ID), nil, admin.accessToken); w.Code != http.StatusNoContent {
		t.Errorf("delete group = %d, want 204", w.Code)
	}
	if w := doJSONAuth(t, h, http.MethodGet, fmt.Sprintf("/api/v1/dict-groups/%d", group.ID), nil, admin.accessToken); w.Code != http.StatusNotFound {
		t.Errorf("get deleted group = %d, want 404", w.Code)
	}
}

func TestDictReplaceValues(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	// Object setting: each field becomes a key/value item (sorted by key).
	w := doJSONAuth(t, h, http.MethodPut, "/api/v1/dicts/site.basic", map[string]any{
		"value": map[string]any{
			"site_url":  "https://example.com",
			"site_name": "Example",
		},
	}, admin.accessToken)
	if w.Code != http.StatusNoContent {
		t.Fatalf("replace object = %d, body=%s", w.Code, w.Body.String())
	}

	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/site.basic", nil, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("read object setting = %d, body=%s", w.Code, w.Body.String())
	}
	var items []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &items); err != nil {
		t.Fatalf("decode object setting: %v", err)
	}
	if len(items) != 2 || items[0].Key != "site_name" || items[0].Value != "Example" ||
		items[1].Key != "site_url" || items[1].Value != "https://example.com" {
		t.Fatalf("object setting items = %+v", items)
	}

	// Array setting: primitive elements keep their order.
	w = doJSONAuth(t, h, http.MethodPut, "/api/v1/dicts/keywords", map[string]any{
		"value": []any{"go", "gin", "gorm"},
	}, admin.accessToken)
	if w.Code != http.StatusNoContent {
		t.Fatalf("replace array = %d, body=%s", w.Code, w.Body.String())
	}
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/keywords", nil, admin.accessToken)
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &items); err != nil {
		t.Fatalf("decode array setting: %v", err)
	}
	if len(items) != 3 || items[0].Key != "go" || items[2].Key != "gorm" {
		t.Fatalf("array setting items = %+v", items)
	}

	// Empty values round-trip as empty: the DTO exposes the stored value, not
	// the key fallback used by dict.Store.GetInto.
	w = doJSONAuth(t, h, http.MethodPut, "/api/v1/dicts/site.empty", map[string]any{
		"value": map[string]any{"unset": ""},
	}, admin.accessToken)
	if w.Code != http.StatusNoContent {
		t.Fatalf("replace empty value = %d, body=%s", w.Code, w.Body.String())
	}
	w = doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/site.empty", nil, admin.accessToken)
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &items); err != nil {
		t.Fatalf("decode empty value: %v", err)
	}
	if len(items) != 1 || items[0].Key != "unset" || items[0].Value != "" {
		t.Fatalf("empty value items = %+v, want raw empty value", items)
	}

	// A scalar is not a valid whole-setting value.
	w = doJSONAuth(t, h, http.MethodPut, "/api/v1/dicts/site.basic", map[string]any{
		"value": "not-an-object-or-array",
	}, admin.accessToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("scalar replace = %d, want 400", w.Code)
	}

	// Anonymous and member writes are rejected.
	if w := doJSON(t, h, http.MethodPut, "/api/v1/dicts/site.basic", map[string]any{
		"value": map[string]any{"site_name": "Hacked"},
	}); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous replace = %d, want 401", w.Code)
	}
}

func TestDictPermissions(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	w := doJSONAuth(t, h, http.MethodPost, "/api/v1/dict-groups", map[string]any{
		"key": "user.status", "name": "User status",
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create group = %d, body=%s", w.Code, w.Body.String())
	}
	var group struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &group); err != nil {
		t.Fatal(err)
	}
	w = doJSONAuth(t, h, http.MethodPost, fmt.Sprintf("/api/v1/dict-groups/%d/items", group.ID), map[string]any{
		"key": "1", "label": "Active",
	}, admin.accessToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create item = %d, body=%s", w.Code, w.Body.String())
	}
	var item struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, w).Data, &item); err != nil {
		t.Fatal(err)
	}

	// Members can consume dicts but cannot manage them.
	memberID, _ := createUser(t, h, admin.accessToken, "Mia", memberEmail)
	w = doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
		map[string]string{"password": memberPass}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("set member password = %d, body=%s", w.Code, w.Body.String())
	}
	member := env.login(t, memberEmail, memberPass, http.StatusOK)

	if w := doJSONAuth(t, h, http.MethodGet, "/api/v1/dicts/user.status", nil, member.accessToken); w.Code != http.StatusOK {
		t.Errorf("member read dict = %d, want 200", w.Code)
	}
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/dict-groups", nil},
		{http.MethodPost, "/api/v1/dict-groups", map[string]any{"key": "x.y", "name": "X"}},
		{http.MethodPatch, fmt.Sprintf("/api/v1/dict-items/%d", item.ID), map[string]any{"label": "Nope"}},
		{http.MethodPut, "/api/v1/dicts/site.basic", map[string]any{"value": map[string]any{"site_name": "Nope"}}},
		{http.MethodDelete, fmt.Sprintf("/api/v1/dict-groups/%d", group.ID), nil},
	} {
		if w := doJSONAuth(t, h, tc.method, tc.path, tc.body, member.accessToken); w.Code != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403", tc.method, tc.path, w.Code)
		}
	}
}

func containsPermission(perms []authkit.Permission, want authkit.Permission) bool {
	for _, perm := range perms {
		if perm == want {
			return true
		}
	}
	return false
}

// TestSecurityHeadersAndNoStoreAreWired 保证中间件真的接在 router 上（单元测试
// 只覆盖中间件本身，接线漏了照样是 0 保护）。
func TestSecurityHeadersAndNoStoreAreWired(t *testing.T) {
	env := newTestEnv(t, false)

	api := doJSON(t, env.handler, http.MethodGet, "/api/v1/auth/me", nil)
	if api.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /auth/me = %d, want 401", api.Code)
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := api.Header().Get(name); got != want {
			t.Errorf("api %s = %q, want %q", name, got, want)
		}
	}
	if got := api.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS outside production = %q, want empty", got)
	}

	ops := doJSON(t, env.handler, http.MethodGet, "/healthz", nil)
	if ops.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", ops.Code)
	}
	if got := ops.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("healthz X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := ops.Header().Get("Cache-Control"); got != "" {
		t.Errorf("healthz Cache-Control = %q, want empty (probes are not API payloads)", got)
	}
}

// TestMetricsEndpointCanBeDisabled 钉住 F7：/metrics 暴露 Go 版本与路由画像，
// 必须有开关能关掉（生产式编排默认就是关的）。
func TestMetricsEndpointCanBeDisabled(t *testing.T) {
	enabled := newTestEnv(t, false) // config.Defaults() → metrics.enabled=true
	if w := doJSON(t, enabled.handler, http.MethodGet, "/metrics", nil); w.Code != http.StatusOK {
		t.Fatalf("/metrics with metrics.enabled=true = %d, want 200", w.Code)
	}

	disabled := newTestEnvWithConfig(t, func(cfg *config.Config) { cfg.Metrics.Enabled = false })
	if w := doJSON(t, disabled.handler, http.MethodGet, "/metrics", nil); w.Code != http.StatusNotFound {
		t.Fatalf("/metrics with metrics.enabled=false = %d, want 404", w.Code)
	}
	// 探针不受影响。
	if w := doJSON(t, disabled.handler, http.MethodGet, "/healthz", nil); w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", w.Code)
	}
}

// TestListEchoesNormalizedPaging 是 F12 的对外契约断言：响应信封里的 page/page_size
// 必须是实际生效的值，不能回显越界的请求参数。
func TestListEchoesNormalizedPaging(t *testing.T) {
	env := newTestEnv(t, false)
	env.seedAdmin(t)
	admin := env.login(t, adminEmail, adminPassword, http.StatusOK)

	w := doJSONAuth(t, env.handler, http.MethodGet, "/api/v1/users?page=-1&page_size=100000", nil, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Page != 1 || body.Data.PageSize != 20 {
		t.Errorf("page/page_size = %d/%d, want 1/20 (normalized)", body.Data.Page, body.Data.PageSize)
	}
}

// TestMemberCannotProbeEmailsViaPatch 是 F9 的对外断言：成员 PATCH 自己的 email
// 时，无论目标地址是否已注册都必须 403（此前是 200/409 二选一 = 枚举接口）。
func TestMemberCannotProbeEmailsViaPatch(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)

	memberID, _ := createUser(t, h, admin.accessToken, "Mia", memberEmail)
	w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
		map[string]string{"password": memberPass}, admin.accessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("set member password = %d", w.Code)
	}
	member := env.login(t, memberEmail, memberPass, http.StatusOK)

	for _, email := range []string{adminEmail, "nobody-free@example.com"} {
		w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
			map[string]string{"email": email}, member.accessToken)
		if w.Code != http.StatusForbidden {
			t.Errorf("member patch email=%s = %d, want 403 (no enumeration oracle)", email, w.Code)
		}
	}
	// 自己的 name 仍然可以改。
	if w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
		map[string]string{"name": "Mia Renamed"}, member.accessToken); w.Code != http.StatusOK {
		t.Errorf("member patch own name = %d, want 200", w.Code)
	}
}

// TestPasswordChangeIsRateLimitedPerUser 钉住 F8：改密端点要跑 bcrypt，必须有自己的
// 预算（按用户而非 IP），否则持有 token 的人可以无限速猜当前密码并烧 CPU。
func TestPasswordChangeIsRateLimitedPerUser(t *testing.T) {
	env := newTestEnvWithConfig(t, func(cfg *config.Config) {
		cfg.RateLimit.Enabled = true // 全局限流开关同时控制改密限流
		cfg.Auth.PasswordRPS = 0.001 // 窗口内不补充，便于断言
		cfg.Auth.PasswordBurst = 2
	})
	h := env.handler
	admin := env.loginAdmin(t)

	body := map[string]string{"current_password": "wrong-password", "new_password": "Whatever-1234"}
	codes := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		w := doJSONAuth(t, h, http.MethodPatch, "/api/v1/auth/password", body, admin.accessToken)
		codes = append(codes, w.Code)
	}
	if codes[0] != http.StatusUnauthorized || codes[1] != http.StatusUnauthorized {
		t.Fatalf("first two attempts = %v, want 401 401", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("third attempt = %d, want 429 (per-user budget exhausted)", codes[2])
	}

	// 其他用户不受影响（按用户分桶，不是全局）。
	env.seedMemberForRateLimitTest(t, h, admin.accessToken)
}

// seedMemberForRateLimitTest 建一个成员并断言其改密请求不被别人的桶挡住。
func (e *testEnv) seedMemberForRateLimitTest(t *testing.T, h http.Handler, adminToken string) {
	t.Helper()
	memberID, _ := createUser(t, h, adminToken, "Rio", "rio@example.com")
	if w := doJSONAuth(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", memberID),
		map[string]string{"password": memberPass}, adminToken); w.Code != http.StatusOK {
		t.Fatalf("set member password = %d", w.Code)
	}
	member := e.login(t, "rio@example.com", memberPass, http.StatusOK)

	w := doJSONAuth(t, h, http.MethodPatch, "/api/v1/auth/password",
		map[string]string{"current_password": "wrong-password", "new_password": "Whatever-1234"}, member.accessToken)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("other user's first attempt = %d, want 401 (own bucket)", w.Code)
	}
}
