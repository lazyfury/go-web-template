package routekit

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

type operation struct {
	method   string
	fullPath string
	specPath string
	id       string
	op       Op
}

// Registry collects route metadata and builds the OpenAPI document. It is not
// safe for concurrent mutation; routes are registered during startup.
type Registry struct {
	cfg  Config
	ops  []*operation
	keys map[string]bool
	ids  map[string]bool
	errs []error
}

// NewRegistry creates a registry and applies defaults to cfg.
//
//nolint:gocritic // Config is declarative metadata and is passed by value.
func NewRegistry(cfg Config) *Registry {
	if cfg.Info.Title == "" {
		cfg.Info.Title = "API"
	}
	if cfg.Info.Version == "" {
		cfg.Info.Version = "1.0.0"
	}
	if cfg.EnvelopeType == nil {
		cfg.EnvelopeType = reflect.TypeFor[httpx.Body]()
	}
	if len(cfg.DefaultErrors) == 0 {
		cfg.DefaultErrors = []apperr.Code{
			apperr.CodePayloadTooLarge,
			apperr.CodeTooManyRequests,
			apperr.CodeInternal,
		}
	}
	return &Registry{
		cfg:  cfg,
		keys: make(map[string]bool),
		ids:  make(map[string]bool),
	}
}

// Add records one operation. fullPath is the complete gin path including the
// group prefix, while specPath is relative to Config.BasePath. It reports
// whether the operation was accepted; failures are returned from Build.
func (r *Registry) Add(method, fullPath, specPath string, op *Op) bool {
	if op == nil {
		r.errs = append(r.errs, fmt.Errorf("routekit: %s %s: nil Op", method, fullPath))
		return false
	}
	method = strings.ToUpper(method)
	if specPath == "" {
		specPath = stripBasePath(fullPath, r.cfg.BasePath)
	}
	specPath = toSpecPath(specPath)
	id := op.ID
	if id == "" {
		id = defaultOperationID(method, specPath)
	}

	if !isSupportedMethod(method) {
		r.errs = append(r.errs, fmt.Errorf("routekit: unsupported HTTP method %q", method))
		return false
	}
	key := method + " " + fullPath
	if r.keys[key] {
		r.errs = append(r.errs, fmt.Errorf("routekit: duplicate route %s", key))
		return false
	}
	if r.ids[id] {
		r.errs = append(r.errs, fmt.Errorf("routekit: duplicate operationId %q (%s %s)", id, method, fullPath))
		return false
	}
	r.keys[key] = true
	r.ids[id] = true
	metadata := *op
	r.ops = append(r.ops, &operation{
		method:   method,
		fullPath: fullPath,
		specPath: specPath,
		id:       id,
		op:       metadata,
	})
	return true
}

// DescribeTag sets the description used for a top-level OpenAPI tag. Calling
// it more than once for the same tag keeps the last description.
func (r *Registry) DescribeTag(name, description string) {
	if name == "" {
		return
	}
	if r.cfg.Tags == nil {
		r.cfg.Tags = make(map[string]string)
	}
	r.cfg.Tags[name] = description
}

// Has reports whether the exact gin route was registered with metadata.
func (r *Registry) Has(method, fullPath string) bool {
	return r.keys[strings.ToUpper(method)+" "+fullPath]
}

// Operations returns the registered routes in a stable order.
func (r *Registry) Operations() []OperationInfo {
	out := make([]OperationInfo, 0, len(r.ops))
	for _, op := range r.ops {
		out = append(out, OperationInfo{
			Method:   op.method,
			Path:     op.fullPath,
			SpecPath: op.specPath,
			ID:       op.id,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Build validates the metadata and returns the OpenAPI document.
func (r *Registry) Build() (*Document, error) {
	if len(r.errs) > 0 {
		return nil, errors.Join(r.errs...)
	}

	ops := make([]*operation, len(r.ops))
	copy(ops, r.ops)
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].specPath == ops[j].specPath {
			return methodOrder(ops[i].method) < methodOrder(ops[j].method)
		}
		return ops[i].specPath < ops[j].specPath
	})

	doc := &Document{
		OpenAPI: "3.0.3",
		Info:    r.cfg.Info,
		Servers: append([]Server(nil), r.cfg.Servers...),
		Tags:    make([]Tag, 0, len(r.cfg.Tags)),
		Paths:   make(map[string]*PathItem),
		Components: &Components{
			Schemas:   make(map[string]*Schema),
			Responses: make(map[string]*ResponseObject),
		},
	}
	if len(r.cfg.SecuritySchemes) > 0 {
		doc.Components.SecuritySchemes = make(map[string]SecurityScheme, len(r.cfg.SecuritySchemes))
		for name, scheme := range r.cfg.SecuritySchemes {
			doc.Components.SecuritySchemes[name] = scheme
		}
	}

	builder := newSchemaBuilder(r.cfg.EnvelopeType)
	tagNames := make(map[string]bool)
	for _, op := range ops {
		built, err := r.buildOperation(builder, doc, op)
		if err != nil {
			return nil, err
		}
		item := doc.Paths[op.specPath]
		if item == nil {
			item = &PathItem{}
			doc.Paths[op.specPath] = item
		}
		if err := item.set(op.method, built); err != nil {
			return nil, err
		}
		for _, tag := range built.Tags {
			tagNames[tag] = true
		}
	}

	if len(builder.components) > 0 {
		doc.Components.Schemas = builder.components
	} else {
		doc.Components.Schemas = nil
	}
	if len(doc.Components.Responses) == 0 {
		doc.Components.Responses = nil
	}
	if len(doc.Components.Schemas) == 0 && len(doc.Components.Responses) == 0 && len(doc.Components.SecuritySchemes) == 0 {
		doc.Components = nil
	}

	names := make([]string, 0, len(tagNames))
	for name := range tagNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		doc.Tags = append(doc.Tags, Tag{Name: name, Description: r.cfg.Tags[name]})
	}
	return doc, nil
}

// JSON builds and marshals the OpenAPI document deterministically.
func (r *Registry) JSON() ([]byte, error) {
	doc, err := r.Build()
	if err != nil {
		return nil, err
	}
	return doc.JSON()
}

func (r *Registry) buildOperation(b *schemaBuilder, doc *Document, op *operation) (*Operation, error) {
	if err := validatePathParams(op.fullPath, op.op.Params); err != nil {
		return nil, fmt.Errorf("routekit: %s %s: %w", op.method, op.fullPath, err)
	}

	built := &Operation{
		ID:          op.id,
		Summary:     op.op.Summary,
		Description: op.op.Description,
		Tags:        dedupeStrings(op.op.Tags),
		Deprecated:  op.op.Deprecated,
		Responses:   make(map[string]*ResponseObject, len(op.op.Responses)+len(op.op.Errors)+len(r.cfg.DefaultErrors)),
	}

	paramKeys := make(map[string]bool)
	for _, param := range op.op.Params {
		if param.Name == "" {
			return nil, fmt.Errorf("routekit: %s %s: parameter with empty name", op.method, op.fullPath)
		}
		in := param.In
		if in == "" {
			in = InQuery
		}
		if !validParamIn(in) {
			return nil, fmt.Errorf("routekit: %s %s: parameter %q has invalid location %q", op.method, op.fullPath, param.Name, in)
		}
		key := string(in) + ":" + param.Name
		if paramKeys[key] {
			return nil, fmt.Errorf("routekit: %s %s: duplicate %s parameter %q", op.method, op.fullPath, in, param.Name)
		}
		paramKeys[key] = true

		schema, err := b.schemaFor(param.Type, modeRequest)
		if err != nil {
			return nil, fmt.Errorf("routekit: %s %s: parameter %q: %w", op.method, op.fullPath, param.Name, err)
		}
		if schema.Type == "object" || schema.Type == "array" || schema.Ref != "" {
			return nil, fmt.Errorf("routekit: %s %s: parameter %q must be a scalar, got %s", op.method, op.fullPath, param.Name, param.Type)
		}
		required := param.Required || in == InPath
		built.Parameters = append(built.Parameters, &Parameter{
			Name:        param.Name,
			In:          string(in),
			Description: param.Description,
			Required:    required,
			Schema:      schema,
			Example:     param.Example,
		})
	}

	if op.op.Request != nil {
		request, err := b.schemaFor(op.op.Request.Type, modeRequest)
		if err != nil {
			return nil, fmt.Errorf("routekit: %s %s: request body: %w", op.method, op.fullPath, err)
		}
		contentType := op.op.Request.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		built.RequestBody = &RequestBodyObject{
			Description: op.op.Request.Description,
			Required:    op.op.Request.Required,
			Content: map[string]*MediaType{
				contentType: {Schema: request, Example: op.op.Request.Example},
			},
		}
	}

	if len(op.op.Responses) == 0 {
		return nil, fmt.Errorf("routekit: %s %s: at least one response is required", op.method, op.fullPath)
	}
	for _, response := range op.op.Responses {
		status := response.Status
		if status == 0 {
			status = http.StatusOK
		}
		if status < 100 || status > 599 {
			return nil, fmt.Errorf("routekit: %s %s: invalid response status %d", op.method, op.fullPath, status)
		}

		obj := &ResponseObject{Description: response.Description}
		if obj.Description == "" {
			obj.Description = http.StatusText(status)
			if obj.Description == "" {
				obj.Description = "response"
			}
		}
		var (
			schema *Schema
			err    error
		)
		switch {
		case response.Envelope:
			schema, err = b.envelopeSchema(response.Data)
		case response.Data != nil:
			schema, err = b.schemaFor(response.Data, modeResponse)
		}
		if err != nil {
			return nil, fmt.Errorf("routekit: %s %s: response %d: %w", op.method, op.fullPath, status, err)
		}
		if status == http.StatusNoContent && schema != nil {
			return nil, fmt.Errorf("routekit: %s %s: 204 response must not have a body", op.method, op.fullPath)
		}
		if schema != nil {
			obj.Content = map[string]*MediaType{
				"application/json": {Schema: schema},
			}
		}
		built.Responses[fmt.Sprintf("%d", status)] = obj
	}

	codes := append([]apperr.Code(nil), op.op.Errors...)
	if !op.op.SkipDefaultErrors {
		codes = append(codes, r.cfg.DefaultErrors...)
	}
	for _, code := range codes {
		if code == apperr.CodeOK {
			return nil, fmt.Errorf("routekit: %s %s: CodeOK is not an error response", op.method, op.fullPath)
		}
		status := code.HTTPStatus()
		key := fmt.Sprintf("%d", status)
		name := "Error" + key
		if _, exists := doc.Components.Responses[name]; !exists {
			schema, err := b.envelopeSchema(nil)
			if err != nil {
				return nil, fmt.Errorf("routekit: %s %s: error response %d: %w", op.method, op.fullPath, status, err)
			}
			if codeProperty, ok := schema.Properties["code"]; ok {
				codeProperty.Example = int(code)
			}
			description := http.StatusText(status)
			if description == "" {
				description = "error"
			}
			doc.Components.Responses[name] = &ResponseObject{
				Description: description,
				Content: map[string]*MediaType{
					"application/json": {Schema: schema},
				},
			}
		}
		if _, exists := built.Responses[key]; !exists {
			built.Responses[key] = &ResponseObject{Ref: "#/components/responses/" + name}
		}
	}

	if len(op.op.Security) > 0 {
		security, err := r.securityRequirement(op)
		if err != nil {
			return nil, err
		}
		built.Security = security
	}
	return built, nil
}

func (r *Registry) securityRequirement(op *operation) ([]map[string][]string, error) {
	security := make([]map[string][]string, 0, len(op.op.Security))
	for _, name := range dedupeStrings(op.op.Security) {
		if name == "" {
			continue
		}
		if _, ok := r.cfg.SecuritySchemes[name]; !ok {
			return nil, fmt.Errorf("routekit: %s %s: unknown security scheme %q", op.method, op.fullPath, name)
		}
		security = append(security, map[string][]string{name: {}})
	}
	return security, nil
}

func validatePathParams(fullPath string, params []Param) error {
	expected := pathParams(fullPath)
	declared := make(map[string]bool, len(expected))
	for _, param := range params {
		if param.In != InPath {
			continue
		}
		if !containsString(expected, param.Name) {
			return fmt.Errorf("path parameter %q is not present in path %q", param.Name, fullPath)
		}
		if declared[param.Name] {
			return fmt.Errorf("duplicate path parameter %q", param.Name)
		}
		declared[param.Name] = true
	}
	for _, name := range expected {
		if !declared[name] {
			return fmt.Errorf("path parameter %q is missing from Op.Params", name)
		}
	}
	return nil
}

func pathParams(fullPath string) []string {
	segments := strings.Split(fullPath, "/")
	names := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch {
		case strings.HasPrefix(segment, ":"):
			names = append(names, strings.TrimPrefix(segment, ":"))
		case strings.HasPrefix(segment, "*"):
			names = append(names, strings.TrimPrefix(segment, "*"))
		}
	}
	return names
}

func isSupportedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete,
		http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace:
		return true
	default:
		return false
	}
}

func validParamIn(in ParamIn) bool {
	switch in {
	case InPath, InQuery, InHeader, InCookie:
		return true
	default:
		return false
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func defaultOperationID(method, specPath string) string {
	return strings.ToLower(method) + camelCase(specPath)
}

func camelCase(path string) string {
	var b strings.Builder
	upper := true
	for _, r := range path {
		if r == '/' || r == '{' || r == '}' || r == '-' || r == '_' || r == '.' || unicode.IsSpace(r) {
			upper = true
			continue
		}
		if upper {
			b.WriteRune(unicode.ToUpper(r))
			upper = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func methodOrder(method string) int {
	switch method {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPut:
		return 2
	case http.MethodPatch:
		return 3
	case http.MethodDelete:
		return 4
	case http.MethodHead:
		return 5
	case http.MethodOptions:
		return 6
	default:
		return 7
	}
}

func joinPaths(parts ...string) string {
	out := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "/") {
			part = "/" + part
		}
		out = strings.TrimSuffix(out, "/") + part
	}
	if out == "" {
		return "/"
	}
	return out
}

func toSpecPath(ginPath string) string {
	segments := strings.Split(ginPath, "/")
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

func stripBasePath(fullPath, basePath string) string {
	basePath = strings.TrimSuffix(basePath, "/")
	if basePath == "" {
		return fullPath
	}
	if fullPath == basePath {
		return "/"
	}
	if strings.HasPrefix(fullPath, basePath+"/") {
		return strings.TrimPrefix(fullPath, basePath)
	}
	return fullPath
}
