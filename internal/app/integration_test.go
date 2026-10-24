package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/auth"
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
	db := testutil.NewSQLiteDB(t, &user.User{}, &auth.RefreshToken{})

	cfg := config.Defaults()
	cfg.RateLimit.Enabled = false
	cfg.Swagger.Enabled = swaggerEnabled
	cfg.Auth.JWTSecret = "integration-test-secret-32-bytes!"
	// cost 4 keeps the suite fast; production uses auth.bcrypt_cost (12).
	cfg.Auth.BcryptCost = 4
	cfg.Auth.CookieSecure = false

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

func TestAnonymousRequestsAreRejected(t *testing.T) {
	env := newTestEnv(t, false)
	h := env.handler

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPost, "/api/v1/users"},
		{http.MethodGet, "/api/v1/users/1"},
		{http.MethodGet, "/api/v1/auth/me"},
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

	// The old refresh cookie is now revoked (reuse detection revokes everything).
	w = doJSONWith(t, env.handler, http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{sess.refresh})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("refresh with the rotated-away cookie = %d, want 401", w.Code)
	}

	// The new access token still works for /auth/me.
	w = doJSONAuth(t, env.handler, http.MethodGet, "/api/v1/auth/me", nil, rotated.AccessToken)
	if w.Code != http.StatusOK {
		t.Errorf("me with refreshed token = %d, want 200", w.Code)
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

func containsPermission(perms []authkit.Permission, want authkit.Permission) bool {
	for _, perm := range perms {
		if perm == want {
			return true
		}
	}
	return false
}
