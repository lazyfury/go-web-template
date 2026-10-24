package routekit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
)

type specCreateRequest struct {
	Name string `json:"name" binding:"required,min=2"`
}

type specUser struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

func newSpecRegistry() *Registry {
	return NewRegistry(Config{
		Info:     Info{Title: "Test API", Version: "1.0.0"},
		BasePath: "/api/v1",
		Tags:     map[string]string{"users": "User management"},
	})
}

func TestBuildOperation(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := newSpecRegistry()
	v1 := NewGroup(engine.Group("/api/v1"), reg)
	users := v1.Group("/users", Tags("users"))

	users.POST("", &Op{
		ID:        "createUser",
		Summary:   "Create a user",
		Request:   JSON[specCreateRequest](true),
		Responses: []Response{Created[specUser]("created user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeConflict},
	}, func(c *gin.Context) { c.Status(http.StatusCreated) })

	users.GET("/:id", &Op{
		Summary:   "Get a user",
		Params:    []Param{Path[uint64]("id", "User ID")},
		Responses: []Response{OK[specUser]("user")},
	}, func(c *gin.Context) {})

	doc, err := reg.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q, want 3.0.3", doc.OpenAPI)
	}
	if !reg.Has(http.MethodPost, "/api/v1/users") {
		t.Error("registry should contain POST /api/v1/users")
	}

	create := doc.Paths["/users"].Post
	if create == nil {
		t.Fatal("missing POST /users operation")
	}
	if create.ID != "createUser" {
		t.Errorf("operationId = %q, want createUser", create.ID)
	}
	if create.RequestBody == nil || !create.RequestBody.Required {
		t.Fatal("request body should be required")
	}
	requestSchema := create.RequestBody.Content["application/json"].Schema
	if requestSchema.Ref != "#/components/schemas/specCreateRequest" {
		t.Errorf("request schema ref = %q", requestSchema.Ref)
	}
	created := create.Responses["201"]
	if created == nil || created.Content == nil {
		t.Fatal("missing 201 response")
	}
	envelope := created.Content["application/json"].Schema
	if envelope.Properties["data"].Ref != "#/components/schemas/specUser" {
		t.Errorf("data ref = %q, want specUser", envelope.Properties["data"].Ref)
	}

	getItem := doc.Paths["/users/{id}"]
	if getItem == nil {
		t.Fatalf("missing /users/{id} path")
	}
	get := getItem.Get
	if get == nil {
		t.Fatal("missing GET /users/{id} operation")
	}
	if get.ID != "getUsersId" {
		t.Errorf("default operationId = %q, want getUsersId", get.ID)
	}
	if len(get.Parameters) != 1 {
		t.Fatalf("parameters = %d, want 1", len(get.Parameters))
	}
	param := get.Parameters[0]
	if param.Name != "id" || param.In != "path" || !param.Required {
		t.Errorf("parameter = %+v, want required path id", param)
	}
	if param.Schema == nil || param.Schema.Type != "integer" {
		t.Errorf("path schema = %+v, want integer", param.Schema)
	}

	for _, name := range []string{"Error400", "Error409", "Error413", "Error429", "Error500"} {
		if _, ok := doc.Components.Responses[name]; !ok {
			t.Errorf("missing error response component %q", name)
		}
	}
	if len(doc.Tags) != 1 || doc.Tags[0].Name != "users" || doc.Tags[0].Description != "User management" {
		t.Errorf("tags = %+v", doc.Tags)
	}
}

func TestBuildRejectsMissingPathParam(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := newSpecRegistry()
	v1 := NewGroup(engine.Group("/api/v1"), reg)
	v1.GET("/users/:id", &Op{
		Responses: []Response{OK[specUser]("user")},
	}, func(c *gin.Context) {})

	if _, err := reg.Build(); err == nil || !strings.Contains(err.Error(), "missing from Op.Params") {
		t.Fatalf("Build error = %v, want missing path parameter", err)
	}
}

func TestBuildRejectsDuplicateOperationID(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := newSpecRegistry()
	v1 := NewGroup(engine.Group("/api/v1"), reg)
	handler := func(c *gin.Context) {}
	v1.GET("/a", &Op{ID: "same", Responses: []Response{OK[specUser]("a")}}, handler)
	v1.GET("/b", &Op{ID: "same", Responses: []Response{OK[specUser]("b")}}, handler)

	if _, err := reg.Build(); err == nil || !strings.Contains(err.Error(), "duplicate operationId") {
		t.Fatalf("Build error = %v, want duplicate operationId", err)
	}
}

func TestBuildDeterministic(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := newSpecRegistry()
	v1 := NewGroup(engine.Group("/api/v1"), reg)
	v1.GET("/users/:id", &Op{
		Params:    []Param{Path[uint64]("id", "User ID")},
		Responses: []Response{OK[specUser]("user")},
	}, func(c *gin.Context) {})

	first, err := reg.JSON()
	if err != nil {
		t.Fatalf("first JSON: %v", err)
	}
	second, err := reg.JSON()
	if err != nil {
		t.Fatalf("second JSON: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("consecutive builds produced different JSON")
	}
}

func TestServeDocs(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := newSpecRegistry()
	v1 := NewGroup(engine.Group("/api/v1"), reg)
	v1.GET("/users", &Op{
		Responses: []Response{OK[[]specUser]("users")},
	}, func(c *gin.Context) {})

	if err := ServeDocs(engine, reg, "/openapi.json", true); err != nil {
		t.Fatalf("ServeDocs: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", http.NoBody)
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var doc Document
	if err := json.Unmarshal(recorder.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q, want 3.0.3", doc.OpenAPI)
	}

	uiRecorder := httptest.NewRecorder()
	uiRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/swagger/index.html", http.NoBody)
	engine.ServeHTTP(uiRecorder, uiRequest)
	if uiRecorder.Code != http.StatusOK {
		t.Errorf("GET /swagger/index.html = %d, want 200", uiRecorder.Code)
	}
}

func TestServeDocsDisabled(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	reg := newSpecRegistry()
	v1 := NewGroup(engine.Group("/api/v1"), reg)
	v1.GET("/users", &Op{
		Responses: []Response{OK[[]specUser]("users")},
	}, func(c *gin.Context) {})

	if err := ServeDocs(engine, reg, "/openapi.json", false); err != nil {
		t.Fatalf("ServeDocs: %v", err)
	}

	for _, path := range []string{"/openapi.json", "/swagger/index.html"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
		engine.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, recorder.Code)
		}
	}
}
