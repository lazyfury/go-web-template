package routekit

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/httpx"
)

type schemaSampleStatus int8

type schemaSample struct {
	ID        uint64             `json:"id"`
	Name      string             `json:"name" binding:"required,min=2,max=64"`
	Email     string             `json:"email,omitempty" binding:"required,email"`
	Status    schemaSampleStatus `json:"status" enum:"0,1"`
	Tags      []string           `json:"tags,omitempty"`
	Meta      map[string]string  `json:"meta,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt *time.Time         `json:"updated_at,omitempty"`
	Secret    string             `json:"-"`
	Count     int                `json:"count,string"`
}

type schemaNode struct {
	Value    string        `json:"value"`
	Children []*schemaNode `json:"children,omitempty"`
}

func resolveSchema(t *testing.T, b *schemaBuilder, schema *Schema) *Schema {
	t.Helper()
	if schema == nil {
		t.Fatal("schema is nil")
	}
	if schema.Ref == "" {
		return schema
	}
	name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
	component, ok := b.components[name]
	if !ok {
		t.Fatalf("component %q not found", name)
	}
	return component
}

func TestSchemaResponseMode(t *testing.T) {
	t.Parallel()

	b := newSchemaBuilder(reflect.TypeFor[httpx.Body]())
	schema, err := b.schemaFor(reflect.TypeFor[schemaSample](), modeResponse)
	if err != nil {
		t.Fatalf("schemaFor: %v", err)
	}
	schema = resolveSchema(t, b, schema)

	if schema.Type != "object" {
		t.Fatalf("type = %q, want object", schema.Type)
	}
	if _, ok := schema.Properties["secret"]; ok {
		t.Error("json:\"-\" field must be omitted")
	}
	if _, ok := schema.Properties["email"]; !ok {
		t.Error("email should still be a property")
	}
	if containsString(schema.Required, "email") {
		t.Error("omitempty response field must not be required")
	}
	for _, name := range []string{"id", "name", "status", "created_at", "count"} {
		if !containsString(schema.Required, name) {
			t.Errorf("%q should be required in a response", name)
		}
	}
	if containsString(schema.Required, "updated_at") {
		t.Error("omitempty pointer must not be required")
	}

	name := schema.Properties["name"]
	if name.MinLength == nil || *name.MinLength != 2 {
		t.Errorf("name.minLength = %v, want 2", name.MinLength)
	}
	if name.MaxLength == nil || *name.MaxLength != 64 {
		t.Errorf("name.maxLength = %v, want 64", name.MaxLength)
	}
	if got := schema.Properties["email"].Format; got != "email" {
		t.Errorf("email.format = %q, want email", got)
	}
	if got := schema.Properties["created_at"].Format; got != "date-time" {
		t.Errorf("created_at.format = %q, want date-time", got)
	}
	if updated := schema.Properties["updated_at"]; !updated.Nullable {
		t.Error("updated_at should be nullable")
	}
	if tags := schema.Properties["tags"]; tags.Type != "array" {
		t.Errorf("tags.type = %q, want array", tags.Type)
	}
	if meta := schema.Properties["meta"]; meta.AdditionalProperties == nil {
		t.Error("meta should use additionalProperties")
	}
	if status := schema.Properties["status"]; len(status.Enum) != 2 {
		t.Errorf("status.enum = %v, want 2 values", status.Enum)
	}
	if count := schema.Properties["count"]; count.Type != "string" {
		t.Errorf("count.type = %q, want string for json:\",string\"", count.Type)
	}
}

func TestSchemaRequestMode(t *testing.T) {
	t.Parallel()

	b := newSchemaBuilder(reflect.TypeFor[httpx.Body]())
	schema, err := b.schemaFor(reflect.TypeFor[schemaSample](), modeRequest)
	if err != nil {
		t.Fatalf("schemaFor: %v", err)
	}
	schema = resolveSchema(t, b, schema)

	if !containsString(schema.Required, "name") || !containsString(schema.Required, "email") {
		t.Errorf("required = %v, want name and email", schema.Required)
	}
	if containsString(schema.Required, "id") {
		t.Error("id must not be required in a request without binding:\"required\"")
	}
}

func TestEnvelopeSchema(t *testing.T) {
	t.Parallel()

	b := newSchemaBuilder(reflect.TypeFor[httpx.Body]())
	schema, err := b.envelopeSchema(reflect.TypeFor[schemaSample]())
	if err != nil {
		t.Fatalf("envelopeSchema: %v", err)
	}
	for _, name := range []string{"code", "message", "data", "request_id"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("missing envelope property %q", name)
		}
	}
	data := schema.Properties["data"]
	if data.Ref != "#/components/schemas/schemaSample" {
		t.Errorf("data.$ref = %q, want schemaSample", data.Ref)
	}
	if containsString(schema.Required, "data") {
		t.Error("data is omitempty and must not be required")
	}

	errorSchema, err := b.envelopeSchema(nil)
	if err != nil {
		t.Fatalf("error envelope: %v", err)
	}
	if _, ok := errorSchema.Properties["data"]; ok {
		t.Error("error envelope must not contain data")
	}
}

func TestSchemaRecursive(t *testing.T) {
	t.Parallel()

	b := newSchemaBuilder(reflect.TypeFor[httpx.Body]())
	schema, err := b.schemaFor(reflect.TypeFor[schemaNode](), modeResponse)
	if err != nil {
		t.Fatalf("schemaFor: %v", err)
	}
	schema = resolveSchema(t, b, schema)
	children := schema.Properties["children"]
	if children.Type != "array" || children.Items == nil {
		t.Fatalf("children = %+v, want array with items", children)
	}
	if children.Items.AllOf == nil || len(children.Items.AllOf) != 1 {
		t.Fatalf("recursive pointer should use allOf, got %+v", children.Items)
	}
	if !children.Items.Nullable {
		t.Error("recursive pointer should be nullable")
	}
}

func TestSchemaAnonymousStruct(t *testing.T) {
	t.Parallel()

	tp := reflect.TypeOf(struct {
		Value string `json:"value"`
	}{})
	b := newSchemaBuilder(reflect.TypeFor[httpx.Body]())
	schema, err := b.schemaFor(tp, modeResponse)
	if err != nil {
		t.Fatalf("schemaFor: %v", err)
	}
	if schema.Ref != "" {
		t.Errorf("anonymous struct should be inlined, got ref %q", schema.Ref)
	}
	if schema.Type != "object" {
		t.Errorf("type = %q, want object", schema.Type)
	}
}

func TestSchemaUnsupportedType(t *testing.T) {
	t.Parallel()

	b := newSchemaBuilder(reflect.TypeFor[httpx.Body]())
	if _, err := b.schemaFor(reflect.TypeFor[chan int](), modeResponse); err == nil {
		t.Fatal("expected an error for chan int")
	}
}
