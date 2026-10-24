package routekit

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/example/go-web-template/internal/platform/httpx"
)

// Schema is an OpenAPI 3.0 schema object (the subset routekit produces).
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	AllOf                []*Schema          `json:"allOf,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Nullable             bool               `json:"nullable,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	MinLength            *int               `json:"minLength,omitempty"`
	MaxLength            *int               `json:"maxLength,omitempty"`
	Example              any                `json:"example,omitempty"`
}

type schemaMode int

const (
	modeResponse schemaMode = iota
	modeRequest
)

type schemaKey struct {
	typ  reflect.Type
	mode schemaMode
}

type schemaBuilder struct {
	envelope   reflect.Type
	components map[string]*Schema
	names      map[schemaKey]string
	owners     map[string]schemaKey
}

var (
	timeType       = reflect.TypeFor[time.Time]()
	rawMessageType = reflect.TypeFor[json.RawMessage]()
	byteType       = reflect.TypeFor[byte]()
)

func newSchemaBuilder(envelope reflect.Type) *schemaBuilder {
	if envelope == nil {
		envelope = reflect.TypeFor[httpx.Body]()
	}
	return &schemaBuilder{
		envelope:   envelope,
		components: make(map[string]*Schema),
		names:      make(map[schemaKey]string),
		owners:     make(map[string]schemaKey),
	}
}

// schemaFor converts a Go type into an OpenAPI 3.0 schema. Request and response
// modes differ in how required fields are derived: requests follow binding
// tags, responses follow json omitempty.
func (b *schemaBuilder) schemaFor(t reflect.Type, mode schemaMode) (*Schema, error) {
	if t == nil {
		return &Schema{}, nil
	}
	if t == timeType {
		return &Schema{Type: "string", Format: "date-time"}, nil
	}
	if t == rawMessageType {
		return &Schema{}, nil
	}

	switch t.Kind() {
	case reflect.Pointer:
		inner, err := b.schemaFor(t.Elem(), mode)
		if err != nil {
			return nil, err
		}
		return nullable(inner), nil
	case reflect.Bool:
		return &Schema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return &Schema{Type: "integer", Format: "int32"}, nil
	case reflect.Int64:
		return &Schema{Type: "integer", Format: "int64"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		zero := 0.0
		return &Schema{Type: "integer", Format: "int64", Minimum: &zero}, nil
	case reflect.Float32:
		return &Schema{Type: "number", Format: "float"}, nil
	case reflect.Float64:
		return &Schema{Type: "number", Format: "double"}, nil
	case reflect.String:
		return &Schema{Type: "string"}, nil
	case reflect.Slice, reflect.Array:
		if t.Elem() == byteType {
			return &Schema{Type: "string", Format: "byte"}, nil
		}
		items, err := b.schemaFor(t.Elem(), mode)
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "array", Items: items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("routekit: map keys must be strings, got %s", t)
		}
		values, err := b.schemaFor(t.Elem(), mode)
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "object", AdditionalProperties: values}, nil
	case reflect.Struct:
		if t.Name() == "" && t.PkgPath() == "" {
			return b.inlineStruct(t, mode)
		}
		return b.refFor(t, mode)
	case reflect.Interface:
		return &Schema{}, nil
	default:
		return nil, fmt.Errorf("routekit: unsupported type %s (%s)", t, t.Kind())
	}
}

func nullable(s *Schema) *Schema {
	if s == nil {
		return nil
	}
	if s.Ref != "" {
		return &Schema{AllOf: []*Schema{{Ref: s.Ref}}, Nullable: true}
	}
	clone := *s
	clone.Nullable = true
	return &clone
}

func (b *schemaBuilder) refFor(t reflect.Type, mode schemaMode) (*Schema, error) {
	name := b.componentName(t, mode)
	if _, ok := b.components[name]; !ok {
		// Register the name before walking fields so recursive types resolve.
		b.components[name] = &Schema{Type: "object"}
		s, err := b.inlineStruct(t, mode)
		if err != nil {
			delete(b.components, name)
			return nil, err
		}
		b.components[name] = s
	}
	return &Schema{Ref: "#/components/schemas/" + name}, nil
}

func (b *schemaBuilder) inlineStruct(t reflect.Type, mode schemaMode) (*Schema, error) {
	s := &Schema{Type: "object", Properties: make(map[string]*Schema)}
	seen := make(map[string]bool)

	for _, field := range reflect.VisibleFields(t) {
		if field.PkgPath != "" {
			continue
		}
		name, opts, hasName := parseJSONTag(field.Tag.Get("json"))
		if name == "-" {
			continue
		}
		// Promoted fields of an untagged embedded struct are already returned
		// by reflect.VisibleFields; the embedding field itself is skipped.
		if field.Anonymous && !hasName {
			continue
		}
		if !hasName {
			name = field.Name
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		prop, err := b.schemaFor(field.Type, mode)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t, field.Name, err)
		}
		hasTags := hasSchemaTags(field.Tag)
		if hasTags {
			prop = taggable(prop)
		}
		applyJSONOptions(prop, opts, field.Type)
		if hasTags {
			applyTags(prop, field.Tag, field.Type)
		}
		s.Properties[name] = prop
		if fieldRequired(field.Tag, opts, mode) {
			s.Required = append(s.Required, name)
		}
	}
	return s, nil
}

type jsonOptions struct {
	omitempty bool
	asString  bool
}

func parseJSONTag(tag string) (string, jsonOptions, bool) {
	if tag == "" {
		return "", jsonOptions{}, false
	}
	parts := strings.Split(tag, ",")
	opts := jsonOptions{}
	for _, opt := range parts[1:] {
		switch opt {
		case "omitempty":
			opts.omitempty = true
		case "string":
			opts.asString = true
		}
	}
	name := parts[0]
	return name, opts, name != ""
}

func fieldRequired(tag reflect.StructTag, opts jsonOptions, mode schemaMode) bool {
	if mode == modeRequest {
		return hasBinding(tag.Get("binding"), "required")
	}
	return !opts.omitempty
}

func hasBinding(binding, name string) bool {
	for _, rule := range strings.Split(binding, ",") {
		if strings.TrimSpace(rule) == name {
			return true
		}
	}
	return false
}

func hasSchemaTags(tag reflect.StructTag) bool {
	for _, name := range [...]string{"description", "format", "example", "enum", "binding"} {
		if tag.Get(name) != "" {
			return true
		}
	}
	return false
}

func taggable(s *Schema) *Schema {
	if s == nil || s.Ref == "" {
		return s
	}
	return &Schema{AllOf: []*Schema{{Ref: s.Ref}}}
}

func applyJSONOptions(s *Schema, opts jsonOptions, t reflect.Type) {
	if !opts.asString || s == nil || s.Type == "" {
		return
	}
	switch baseKind(t) {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		s.Type = "string"
		s.Format = ""
	}
}

func applyTags(s *Schema, tag reflect.StructTag, t reflect.Type) {
	if s == nil {
		return
	}
	if v := tag.Get("description"); v != "" {
		s.Description = v
	}
	if v := tag.Get("format"); v != "" {
		s.Format = v
	}
	if v := tag.Get("example"); v != "" {
		s.Example = parseScalar(v, t)
	}
	if v := tag.Get("enum"); v != "" {
		s.Enum = parseEnum(strings.Split(v, ","), t)
	}
	applyBinding(s, tag.Get("binding"), t)
}

func applyBinding(s *Schema, binding string, t reflect.Type) {
	for _, rule := range strings.Split(binding, ",") {
		rule = strings.TrimSpace(rule)
		if rule == "" || rule == "required" {
			continue
		}
		key, value, hasValue := strings.Cut(rule, "=")
		switch key {
		case "min":
			applyBound(s, t, value, true, false)
		case "max":
			applyBound(s, t, value, false, true)
		case "len":
			applyBound(s, t, value, true, true)
		case "email":
			s.Format = "email"
		case "uuid":
			s.Format = "uuid"
		case "url", "uri":
			s.Format = "uri"
		case "oneof":
			if hasValue {
				s.Enum = parseEnum(strings.Fields(value), t)
			}
		}
	}
}

func applyBound(s *Schema, t reflect.Type, raw string, lower, upper bool) {
	if !lower && !upper {
		return
	}
	switch baseKind(t) {
	case reflect.String:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return
		}
		if lower {
			s.MinLength = &n
		}
		if upper {
			s.MaxLength = &n
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return
		}
		if lower {
			s.Minimum = &n
		}
		if upper {
			s.Maximum = &n
		}
	}
}

func baseKind(t reflect.Type) reflect.Kind {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return reflect.Invalid
	}
	return t.Kind()
}

func parseEnum(values []string, t reflect.Type) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == "" {
			continue
		}
		out = append(out, parseScalar(value, t))
	}
	return out
}

func parseScalar(raw string, t reflect.Type) any {
	switch baseKind(t) {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return n
		}
	case reflect.Float32, reflect.Float64:
		if n, err := strconv.ParseFloat(raw, 64); err == nil {
			return n
		}
	case reflect.Bool:
		if n, err := strconv.ParseBool(raw); err == nil {
			return n
		}
	}
	return raw
}

func (b *schemaBuilder) componentName(t reflect.Type, mode schemaMode) string {
	key := schemaKey{typ: t, mode: mode}
	if name, ok := b.names[key]; ok {
		return name
	}

	base := baseComponentName(t)
	name := base
	if owner, exists := b.owners[name]; exists && owner != key {
		suffix := "Request"
		if mode == modeResponse {
			suffix = "Response"
		}
		name = base + suffix
		for i := 2; ; i++ {
			if _, exists := b.owners[name]; !exists {
				break
			}
			name = fmt.Sprintf("%s%s%d", base, suffix, i)
		}
	}
	b.names[key] = name
	b.owners[name] = key
	return name
}

func baseComponentName(t reflect.Type) string {
	if t.Name() != "" {
		return sanitizeName(t.Name())
	}
	if name := sanitizeName(t.String()); name != "" {
		return name
	}
	return "Anonymous"
}

func sanitizeName(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	if first, _ := utf8.DecodeRuneInString(out); unicode.IsDigit(first) {
		out = "T" + out
	}
	return out
}

// envelopeSchema builds the response envelope from the configured struct. The
// JSON field named "data" is replaced with the response payload type.
func (b *schemaBuilder) envelopeSchema(data reflect.Type) (*Schema, error) {
	if b.envelope == nil {
		return nil, fmt.Errorf("routekit: envelope type is not configured")
	}
	if b.envelope.Kind() != reflect.Struct {
		return nil, fmt.Errorf("routekit: envelope type must be a struct, got %s", b.envelope)
	}

	s := &Schema{Type: "object", Properties: make(map[string]*Schema)}
	for _, field := range reflect.VisibleFields(b.envelope) {
		if field.PkgPath != "" {
			continue
		}
		name, opts, hasName := parseJSONTag(field.Tag.Get("json"))
		if name == "-" {
			continue
		}
		if field.Anonymous && !hasName {
			continue
		}
		if !hasName {
			name = field.Name
		}

		var (
			prop *Schema
			err  error
		)
		if name == "data" {
			if data == nil {
				continue
			}
			prop, err = b.schemaFor(data, modeResponse)
		} else {
			prop, err = b.schemaFor(field.Type, modeResponse)
		}
		if err != nil {
			return nil, fmt.Errorf("envelope.%s: %w", field.Name, err)
		}
		s.Properties[name] = prop
		if !opts.omitempty {
			s.Required = append(s.Required, name)
		}
	}
	return s, nil
}
