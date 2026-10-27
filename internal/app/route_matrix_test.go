package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/message"
)

// The route matrix is the anti-drift gate for the authorization contract:
// every registered operation must declare what an anonymous caller, a member
// and an admin get, plus what an invalid body gets. A new route fails
// TestRouteMatrixCoversEveryRegisteredRoute until it is classified here.
//
// This is deliberately a matrix of *routes*, not of statements: coverage may
// look fine while a single admin-only endpoint silently answers 200 to a
// member, and no unit test would notice.

// routeFixture is the smallest dataset that lets every route reach its service
// layer instead of bouncing off "not found".
type routeFixture struct {
	env    *testEnv
	h      http.Handler
	admin  *session
	member *session

	victimID   uint64
	groupID    uint64
	itemID     uint64
	templateID uint64
	messageID  uint64
	deliveryID uint64
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	env := newTestEnv(t, false)
	h := env.handler
	admin := env.loginAdmin(t)
	f := &routeFixture{env: env, h: h, admin: admin}

	memberID := env.createMember(t, admin, memberEmail, "Matrix Member", "+10000000000")
	f.member = env.login(t, memberEmail, memberPass, http.StatusOK)
	f.victimID, _ = createUser(t, h, admin.accessToken, "Matrix Victim", "victim@example.com")

	// One dict group with one item: covers every /dict-groups and /dict-items route.
	f.groupID = idFromResponse(t, doJSONAuth(t, h, http.MethodPost, "/api/v1/dict-groups",
		map[string]any{"key": "matrix.status", "name": "Matrix status"}, admin.accessToken))
	f.itemID = idFromResponse(t, doJSONAuth(t, h, http.MethodPost,
		fmt.Sprintf("/api/v1/dict-groups/%d/items", f.groupID),
		map[string]any{"key": "1", "label": "Active", "value": "1", "sort": 10}, admin.accessToken))

	// One template: covers GET/PATCH/DELETE /message-templates/:id.
	f.templateID = idFromResponse(t, doJSONAuth(t, h, http.MethodPost, "/api/v1/message-templates",
		map[string]any{
			"key": "matrix.welcome", "channel": "inbox", "locale": "zh-CN",
			"subject": "Hi {{.Name}}", "body": "Welcome {{.Name}}", "variables": []string{"Name"},
		}, admin.accessToken))

	// One multi-channel message: inbox entry, sent row and three deliveries.
	doJSONAuth(t, h, http.MethodPost, "/api/v1/messages", map[string]any{
		"title":      "Matrix",
		"body":       "hello",
		"type":       "system",
		"level":      "info",
		"channels":   []string{"inbox", "email", "sms", "webhook"},
		"recipients": map[string]any{"user_ids": []uint64{memberID}},
	}, admin.accessToken)

	inbox := doJSONAuth(t, h, http.MethodGet, "/api/v1/messages/inbox", nil, f.member.accessToken)
	var inboxPage struct {
		Items []struct {
			ID uint64 `json:"id"`
		} `json:"items"`
	}
	decodeInto(t, inbox, &inboxPage)
	if len(inboxPage.Items) != 1 {
		t.Fatalf("fixture inbox has %d items, want 1", len(inboxPage.Items))
	}
	f.messageID = inboxPage.Items[0].ID

	deliveries := doJSONAuth(t, h, http.MethodGet, "/api/v1/message-deliveries", nil, admin.accessToken)
	var deliveryPage struct {
		Items []struct {
			ID uint64 `json:"id"`
		} `json:"items"`
	}
	decodeInto(t, deliveries, &deliveryPage)
	if len(deliveryPage.Items) == 0 {
		t.Fatal("fixture produced no deliveries")
	}
	f.deliveryID = deliveryPage.Items[0].ID
	// Retry only accepts failed/dead rows; park one delivery there so the
	// success path is exercised instead of only the 409 branch.
	if err := env.db.Model(&message.Delivery{}).
		Where("id = ?", f.deliveryID).
		Update("status", message.DeliveryFailed).Error; err != nil {
		t.Fatalf("mark delivery failed: %v", err)
	}
	return f
}

func idFromResponse(t *testing.T, w *httptest.ResponseRecorder) uint64 {
	t.Helper()
	if w.Code != http.StatusCreated {
		t.Fatalf("fixture create status = %d, body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		ID uint64 `json:"id"`
	}
	decodeInto(t, w, &payload)
	if payload.ID == 0 {
		t.Fatalf("fixture create returned no id: %s", w.Body.String())
	}
	return payload.ID
}

func decodeInto(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()
	env := decodeEnvelope(t, w)
	if err := json.Unmarshal(env.Data, target); err != nil {
		t.Fatalf("decode data: %v; body=%s", err, w.Body.String())
	}
}

// routeCase declares the contract for one operation.
type routeCase struct {
	name   string
	spec   string // gin-style path, used by the completeness guard
	method string
	path   func(f *routeFixture) string
	body   func(f *routeFixture) any
	// adminBody overrides body for the admin probe when success depends on the
	// caller's own credentials (change password).
	adminBody func(f *routeFixture) any

	// public operations are skipped for the member/admin probes: there is no
	// role to compare, only the unauthenticated answer.
	public bool
	// cookie tells the probe to send the caller's refresh cookie (logout,
	// refresh-style endpoints). Without it those routes would answer 401 for
	// the wrong reason.
	cookie bool

	anon   int
	member int
	admin  int
}

func routeMatrix() []routeCase {
	return []routeCase{
		// ---- public ------------------------------------------------------
		{name: "login", spec: "/api/v1/auth/login", method: http.MethodPost,
			path:   constPath("/api/v1/auth/login"),
			body:   bodyOf(map[string]string{"email": adminEmail, "password": "wrong-password"}),
			public: true, anon: http.StatusUnauthorized},
		{name: "refresh without cookie", spec: "/api/v1/auth/refresh", method: http.MethodPost,
			path:   constPath("/api/v1/auth/refresh"),
			public: true, anon: http.StatusUnauthorized},

		// ---- auth --------------------------------------------------------
		{name: "logout", spec: "/api/v1/auth/logout", method: http.MethodPost,
			path: constPath("/api/v1/auth/logout"), cookie: true,
			member: http.StatusNoContent, admin: http.StatusNoContent},
		{name: "me", spec: "/api/v1/auth/me", method: http.MethodGet,
			path:   constPath("/api/v1/auth/me"),
			member: http.StatusOK, admin: http.StatusOK},
		{name: "change password", spec: "/api/v1/auth/password", method: http.MethodPatch,
			path:      constPath("/api/v1/auth/password"),
			body:      bodyOf(map[string]string{"current_password": memberPass, "new_password": "matrix-password"}),
			adminBody: bodyOf(map[string]string{"current_password": adminPassword, "new_password": "matrix-password"}),
			member:    http.StatusNoContent, admin: http.StatusNoContent},

		// ---- users -------------------------------------------------------
		{name: "create user", spec: "/api/v1/users", method: http.MethodPost,
			path:   constPath("/api/v1/users"),
			body:   bodyOf(map[string]any{"name": "Matrix Created", "email": "matrix-created@example.com", "password": "initial-password"}),
			member: http.StatusForbidden, admin: http.StatusCreated},
		{name: "list users", spec: "/api/v1/users", method: http.MethodGet,
			path:   constPath("/api/v1/users"),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "get user", spec: "/api/v1/users/:id", method: http.MethodGet,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/users/%d", f.victimID) },
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "update user", spec: "/api/v1/users/:id", method: http.MethodPatch,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/users/%d", f.victimID) },
			body:   bodyOf(map[string]any{"name": "Matrix Renamed"}),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "delete user", spec: "/api/v1/users/:id", method: http.MethodDelete,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/users/%d", f.victimID) },
			member: http.StatusForbidden, admin: http.StatusNoContent},

		// ---- dicts -------------------------------------------------------
		{name: "get dict values", spec: "/api/v1/dicts/:group", method: http.MethodGet,
			path:   constPath("/api/v1/dicts/matrix.status"),
			member: http.StatusOK, admin: http.StatusOK},
		{name: "replace dict values", spec: "/api/v1/dicts/:group", method: http.MethodPut,
			path:   constPath("/api/v1/dicts/matrix.status"),
			body:   bodyOf(map[string]any{"value": map[string]any{"site_name": "Matrix"}}),
			member: http.StatusForbidden, admin: http.StatusNoContent},
		{name: "create dict group", spec: "/api/v1/dict-groups", method: http.MethodPost,
			path:   constPath("/api/v1/dict-groups"),
			body:   bodyOf(map[string]any{"key": "matrix.created", "name": "Matrix created"}),
			member: http.StatusForbidden, admin: http.StatusCreated},
		{name: "list dict groups", spec: "/api/v1/dict-groups", method: http.MethodGet,
			path:   constPath("/api/v1/dict-groups"),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "get dict group", spec: "/api/v1/dict-groups/:id", method: http.MethodGet,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-groups/%d", f.groupID) },
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "update dict group", spec: "/api/v1/dict-groups/:id", method: http.MethodPatch,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-groups/%d", f.groupID) },
			body:   bodyOf(map[string]any{"name": "Matrix renamed"}),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "delete dict group", spec: "/api/v1/dict-groups/:id", method: http.MethodDelete,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-groups/%d", f.groupID) },
			member: http.StatusForbidden, admin: http.StatusNoContent},
		{name: "list dict items", spec: "/api/v1/dict-groups/:id/items", method: http.MethodGet,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-groups/%d/items", f.groupID) },
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "create dict item", spec: "/api/v1/dict-groups/:id/items", method: http.MethodPost,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-groups/%d/items", f.groupID) },
			body:   bodyOf(map[string]any{"key": "2", "label": "Suspended", "value": "2", "sort": 20}),
			member: http.StatusForbidden, admin: http.StatusCreated},
		{name: "update dict item", spec: "/api/v1/dict-items/:id", method: http.MethodPatch,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-items/%d", f.itemID) },
			body:   bodyOf(map[string]any{"label": "Matrix item"}),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "delete dict item", spec: "/api/v1/dict-items/:id", method: http.MethodDelete,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/dict-items/%d", f.itemID) },
			member: http.StatusForbidden, admin: http.StatusNoContent},

		// ---- messages ----------------------------------------------------
		{name: "send message", spec: "/api/v1/messages", method: http.MethodPost,
			path: constPath("/api/v1/messages"),
			body: func(f *routeFixture) any {
				return map[string]any{
					"title": "Matrix send", "body": "hello", "type": "system", "level": "info",
					"channels": []string{"inbox"}, "recipients": map[string]any{"user_ids": []uint64{f.member.user.ID}},
				}
			},
			member: http.StatusForbidden, admin: http.StatusCreated},
		{name: "list sent messages", spec: "/api/v1/messages/sent", method: http.MethodGet,
			path:   constPath("/api/v1/messages/sent"),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "list inbox", spec: "/api/v1/messages/inbox", method: http.MethodGet,
			path:   constPath("/api/v1/messages/inbox"),
			member: http.StatusOK, admin: http.StatusOK},
		{name: "unread count", spec: "/api/v1/messages/unread-count", method: http.MethodGet,
			path:   constPath("/api/v1/messages/unread-count"),
			member: http.StatusOK, admin: http.StatusOK},
		{name: "get inbox message", spec: "/api/v1/messages/inbox/:id", method: http.MethodGet,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/messages/inbox/%d", f.messageID) },
			member: http.StatusOK, admin: http.StatusNotFound},
		{name: "mark message read", spec: "/api/v1/messages/inbox/:id/read", method: http.MethodPatch,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/messages/inbox/%d/read", f.messageID) },
			member: http.StatusNoContent, admin: http.StatusNotFound},
		{name: "mark all read", spec: "/api/v1/messages/read-all", method: http.MethodPost,
			path:   constPath("/api/v1/messages/read-all"),
			member: http.StatusNoContent, admin: http.StatusNoContent},
		{name: "delete inbox message", spec: "/api/v1/messages/inbox/:id", method: http.MethodDelete,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/messages/inbox/%d", f.messageID) },
			member: http.StatusNoContent, admin: http.StatusNotFound},

		// ---- templates ---------------------------------------------------
		{name: "list templates", spec: "/api/v1/message-templates", method: http.MethodGet,
			path:   constPath("/api/v1/message-templates"),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "create template", spec: "/api/v1/message-templates", method: http.MethodPost,
			path: constPath("/api/v1/message-templates"),
			body: bodyOf(map[string]any{
				"key": "matrix.created", "channel": "inbox", "locale": "zh-CN",
				"subject": "Hi", "body": "Hello",
			}),
			member: http.StatusForbidden, admin: http.StatusCreated},
		{name: "get template", spec: "/api/v1/message-templates/:id", method: http.MethodGet,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/message-templates/%d", f.templateID) },
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "update template", spec: "/api/v1/message-templates/:id", method: http.MethodPatch,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/message-templates/%d", f.templateID) },
			body:   bodyOf(map[string]any{"subject": "Hello {{.Name}}"}),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "delete template", spec: "/api/v1/message-templates/:id", method: http.MethodDelete,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/message-templates/%d", f.templateID) },
			member: http.StatusForbidden, admin: http.StatusNoContent},

		// ---- deliveries --------------------------------------------------
		{name: "list deliveries", spec: "/api/v1/message-deliveries", method: http.MethodGet,
			path:   constPath("/api/v1/message-deliveries"),
			member: http.StatusForbidden, admin: http.StatusOK},
		{name: "retry delivery", spec: "/api/v1/message-deliveries/:id/retry", method: http.MethodPost,
			path:   func(f *routeFixture) string { return fmt.Sprintf("/api/v1/message-deliveries/%d/retry", f.deliveryID) },
			member: http.StatusForbidden, admin: http.StatusNoContent},
	}
}

func constPath(path string) func(*routeFixture) string {
	return func(*routeFixture) string { return path }
}

func bodyOf(body any) func(*routeFixture) any {
	return func(*routeFixture) any { return body }
}

func TestRouteMatrix(t *testing.T) {
	for _, tc := range routeMatrix() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRouteFixture(t)

			method, path := tc.method, tc.path(f)
			var body any
			if tc.body != nil {
				body = tc.body(f)
			}

			// 1. Anonymous: every protected operation answers 401, not 403/404
			//    (an unauthenticated caller must not learn whether an id exists).
			wantAnon := tc.anon
			if wantAnon == 0 {
				wantAnon = http.StatusUnauthorized
			}
			w := doJSON(t, f.h, method, path, body)
			if w.Code != wantAnon {
				t.Errorf("anonymous %s %s = %d, want %d, body=%s", method, path, w.Code, wantAnon, w.Body.String())
			}
			if tc.public {
				return
			}

			// 2. Authenticated but malformed JSON: 400 from ShouldBindJSON, never
			//    a 500 and never a silent success.
			if tc.body != nil {
				req := httptest.NewRequestWithContext(context.Background(), method, path, bytes.NewBufferString("{"))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+f.admin.accessToken)
				w = httptest.NewRecorder()
				f.h.ServeHTTP(w, req)
				if w.Code != http.StatusBadRequest {
					t.Errorf("anonymous-id admin %s %s with invalid JSON = %d, want 400, body=%s", method, path, w.Code, w.Body.String())
				}
			}
			// 3. Member: either an explicit permission failure (403) or the
			//    member-scoped answer.
			memberCookies := callerCookies(&tc, f.member)
			w = doJSONWith(t, f.h, method, path, body, f.member.accessToken, memberCookies)
			if w.Code != tc.member {
				t.Errorf("member %s %s = %d, want %d, body=%s", method, path, w.Code, tc.member, w.Body.String())
			}

			// 4. Admin: reaches the service and succeeds. Runs last because some
			//    operations (change password) revoke the caller's own tokens.
			adminCookies := callerCookies(&tc, f.admin)
			adminBody := body
			if tc.adminBody != nil {
				adminBody = tc.adminBody(f)
			}
			w = doJSONWith(t, f.h, method, path, adminBody, f.admin.accessToken, adminCookies)
			if w.Code != tc.admin {
				t.Errorf("admin %s %s = %d, want %d, body=%s", method, path, w.Code, tc.admin, w.Body.String())
			}
		})
	}
}

func callerCookies(tc *routeCase, s *session) []*http.Cookie {
	if !tc.cookie {
		return nil
	}
	return []*http.Cookie{s.refresh}
}

// TestRouteMatrixCoversEveryRegisteredRoute keeps the matrix honest: adding a
// route without classifying it here fails, so the 401/403 contract cannot
// silently grow a hole.
func TestRouteMatrixCoversEveryRegisteredRoute(t *testing.T) {
	application := newTestApplication(t, true)
	engine, ok := application.Handler().(*gin.Engine)
	if !ok {
		t.Fatal("handler is not a *gin.Engine")
	}

	classified := make(map[string]bool, len(routeMatrix()))
	for _, tc := range routeMatrix() {
		classified[tc.method+" "+ginPathToSpecPath(tc.spec)] = true
	}

	for _, route := range engine.Routes() {
		if isUndocumentedRoute(route.Path) {
			continue
		}
		key := route.Method + " " + ginPathToSpecPath(route.Path)
		if !classified[key] {
			t.Errorf("route %s is not classified in routeMatrix() (add its anon/member/admin expectations)", key)
		}
	}
}
