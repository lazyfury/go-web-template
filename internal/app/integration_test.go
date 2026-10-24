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

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/testutil"
	"github.com/example/go-web-template/internal/user"
)

// newTestApplication exercises the real composition root against an
// in-memory SQLite database: router → middleware → handler → service →
// repository. Because it calls app.NewWithDB, a wiring mistake in app.go
// fails here too.
func newTestApplication(t *testing.T, swaggerEnabled bool) *app.App {
	t.Helper()
	db := testutil.NewSQLiteDB(t, &user.User{})

	cfg := config.Defaults()
	cfg.RateLimit.Enabled = false
	cfg.Swagger.Enabled = swaggerEnabled

	application, err := app.NewWithDB(cfg, zap.NewNop(), db)
	if err != nil {
		t.Fatalf("assemble app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return newTestApplication(t, false).Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
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

func createUser(t *testing.T, h http.Handler, name, email string) (userID uint64, statusCode int) {
	t.Helper()
	w := doJSON(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": name, "email": email,
	})
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
	h := newTestHandler(t)

	// Create.
	id, _ := createUser(t, h, "Alice", "alice@example.com")
	if id == 0 {
		t.Fatal("created user id should not be zero")
	}

	// Duplicate email → 409 conflict.
	w := doJSON(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": "Alice2", "email": "ALICE@example.com",
	})
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate create status = %d, want 409", w.Code)
	}

	// Validation error → 400.
	w = doJSON(t, h, http.MethodPost, "/api/v1/users", map[string]string{
		"name": "", "email": "not-an-email",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid create status = %d, want 400", w.Code)
	}

	// Get.
	w = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", id), nil)
	if w.Code != http.StatusOK {
		t.Errorf("get status = %d, body=%s", w.Code, w.Body.String())
	}

	// Update.
	w = doJSON(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", id), map[string]string{
		"name": "Alicia",
	})
	if w.Code != http.StatusOK {
		t.Errorf("update status = %d, body=%s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w)
	var updated map[string]any
	if err := json.Unmarshal(env.Data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated["name"] != "Alicia" {
		t.Errorf("updated name = %v, want Alicia", updated["name"])
	}

	// List with keyword.
	w = doJSON(t, h, http.MethodGet, "/api/v1/users?keyword=alicia&page=1&page_size=10", nil)
	if w.Code != http.StatusOK {
		t.Errorf("list status = %d", w.Code)
	}
	env = decodeEnvelope(t, w)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(env.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Errorf("list total = %d items=%d, want 1/1", page.Total, len(page.Items))
	}

	// Delete.
	w = doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", id), nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", w.Code)
	}

	// Get after delete → 404.
	w = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", id), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete status = %d, want 404", w.Code)
	}

	// Invalid id → 400.
	w = doJSON(t, h, http.MethodGet, "/api/v1/users/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", w.Code)
	}
}

func TestHealthEndpoints(t *testing.T) {
	h := newTestHandler(t)

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
	h := newTestHandler(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("X-Request-ID", "test-request-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "test-request-123" {
		t.Errorf("request id = %q, want test-request-123", got)
	}
}

func TestNoRoute(t *testing.T) {
	h := newTestHandler(t)
	w := doJSON(t, h, http.MethodGet, "/api/v1/nope", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("no-route status = %d, want 404", w.Code)
	}
}
