package app_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/app"
)

var updateOpenAPI = flag.Bool("update", false, "update docs/openapi.json")

func openAPIJSON(t *testing.T, application *app.App) []byte {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", http.NoBody)
	application.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.Bytes()
}

func TestOpenAPIDocument(t *testing.T) {
	application := newTestApplication(t, true)
	raw := openAPIJSON(t, application)

	var doc struct {
		OpenAPI    string                                `json:"openapi"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q, want 3.0.3", doc.OpenAPI)
	}
	for _, path := range []string{"/users", "/users/{id}"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("missing path %q", path)
		}
	}
	for _, schema := range []string{
		"CreateRequest", "UpdateRequest", "UserDTO", "ListResponse",
		"CreateGroupRequest", "UpdateGroupRequest", "GroupDTO", "GroupListResponse",
		"CreateItemRequest", "UpdateItemRequest", "ItemDTO",
		"SendRequest", "SendResultDTO", "InboxResponse", "InboxItemDTO",
		"SentListResponse", "TemplateDTO", "TemplateListResponse",
		"DeliveryDTO", "DeliveryListResponse",
	} {
		if _, ok := doc.Components.Schemas[schema]; !ok {
			t.Errorf("missing component schema %q", schema)
		}
	}

	// The envelope data must reference the real response type instead of
	// being untyped (the old swagger output showed httpx.Body.data as empty).
	var create struct {
		Responses map[string]struct {
			Content map[string]struct {
				Schema struct {
					Properties map[string]struct {
						Ref string `json:"$ref"`
					} `json:"properties"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(doc.Paths["/users"]["post"], &create); err != nil {
		t.Fatalf("decode POST /users: %v", err)
	}
	dataRef := create.Responses["201"].Content["application/json"].Schema.Properties["data"].Ref
	if dataRef != "#/components/schemas/UserDTO" {
		t.Errorf("201 data $ref = %q, want #/components/schemas/UserDTO", dataRef)
	}
}

func TestOpenAPICoversAllRoutes(t *testing.T) {
	application := newTestApplication(t, true)
	engine, ok := application.Handler().(*gin.Engine)
	if !ok {
		t.Fatal("handler is not a *gin.Engine")
	}

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openAPIJSON(t, application), &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	documented := make(map[string]bool)
	for path, methods := range doc.Paths {
		for method := range methods {
			documented[strings.ToUpper(method)+" /api/v1"+path] = true
		}
	}

	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		if isUndocumentedRoute(route.Path) {
			continue
		}
		key := route.Method + " " + ginPathToSpecPath(route.Path)
		registered[key] = true
		if !documented[key] {
			t.Errorf("gin route %s is missing from docs/openapi.json", key)
		}
	}
	for key := range documented {
		if !registered[key] {
			t.Errorf("documented operation %s has no gin route", key)
		}
	}
}

func ginPathToSpecPath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		switch {
		case strings.HasPrefix(segment, ":"):
			segments[i] = "{" + strings.TrimPrefix(segment, ":") + "}"
		case strings.HasPrefix(segment, "*"):
			segments[i] = "{" + strings.TrimPrefix(segment, "*") + "}"
		}
	}
	return strings.Join(segments, "/")
}

func isUndocumentedRoute(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/metrics", "/openapi.json":
		return true
	}
	// Signed local-download URLs are capability URLs (HMAC in the path), not API
	// operations: they are deliberately absent from the OpenAPI document.
	return strings.HasPrefix(path, "/swagger/") || strings.HasPrefix(path, "/files/")
}

func TestOpenAPIDisabled(t *testing.T) {
	application := newTestApplication(t, false)
	for _, path := range []string{"/openapi.json", "/swagger/index.html"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
		application.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, recorder.Code)
		}
	}
}

func TestOpenAPIGolden(t *testing.T) {
	application := newTestApplication(t, true)
	raw := openAPIJSON(t, application)

	path := filepath.Join("..", "..", "docs", "openapi.json")
	if *updateOpenAPI {
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run make openapi): %v", path, err)
	}
	if !bytes.Equal(raw, want) {
		t.Error("OpenAPI document differs from docs/openapi.json; run make openapi and review the diff")
	}
}

func TestOpenAPISecurity(t *testing.T) {
	application := newTestApplication(t, true)
	raw := openAPIJSON(t, application)

	var doc struct {
		Paths map[string]map[string]struct {
			Security  []map[string][]string      `json:"security"`
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type         string `json:"type"`
				Scheme       string `json:"scheme"`
				BearerFormat string `json:"bearerFormat"`
			} `json:"securitySchemes"`
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	scheme, ok := doc.Components.SecuritySchemes["bearerAuth"]
	if !ok {
		t.Fatal("missing bearerAuth security scheme")
	}
	if scheme.Type != "http" || scheme.Scheme != "bearer" || scheme.BearerFormat != "JWT" {
		t.Errorf("bearerAuth = %+v", scheme)
	}

	// Every user operation is protected, documents 401/403 and the caller's
	// permission failure mode.
	for path, item := range doc.Paths {
		if !isProtectedDomainPath(path) {
			continue
		}
		for method, op := range item {
			if !hasBearer(op.Security) {
				t.Errorf("%s %s: missing bearerAuth security", strings.ToUpper(method), path)
			}
			if _, ok := op.Responses["401"]; !ok {
				t.Errorf("%s %s: missing documented 401", strings.ToUpper(method), path)
			}
			if _, ok := op.Responses["403"]; !ok {
				t.Errorf("%s %s: missing documented 403", strings.ToUpper(method), path)
			}
		}
	}

	// Public endpoints must stay public in the contract.
	for _, path := range []string{"/auth/login", "/auth/refresh"} {
		for method, op := range doc.Paths[path] {
			if len(op.Security) != 0 {
				t.Errorf("%s %s must not require a bearer token", strings.ToUpper(method), path)
			}
		}
	}

	// The auth schemas must exist so the frontend can generate types.
	for _, schema := range []string{"LoginRequest", "LoginResponse", "ChangePasswordRequest", "UserView"} {
		if _, ok := doc.Components.Schemas[schema]; !ok {
			t.Errorf("missing component schema %q", schema)
		}
	}
}

func isProtectedDomainPath(path string) bool {
	for _, prefix := range []string{"/users", "/dicts", "/dict-groups", "/dict-items", "/messages", "/message-templates", "/message-deliveries"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func hasBearer(security []map[string][]string) bool {
	for _, requirement := range security {
		if _, ok := requirement["bearerAuth"]; ok {
			return true
		}
	}
	return false
}
