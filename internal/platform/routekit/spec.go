package routekit

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Document is the generated OpenAPI 3.0 document.
type Document struct {
	OpenAPI    string               `json:"openapi"`
	Info       Info                 `json:"info"`
	Servers    []Server             `json:"servers,omitempty"`
	Tags       []Tag                `json:"tags,omitempty"`
	Paths      map[string]*PathItem `json:"paths"`
	Components *Components          `json:"components,omitempty"`
}

// Tag is a top-level OpenAPI tag.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PathItem groups the operations available on one path.
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Options *Operation `json:"options,omitempty"`
	Head    *Operation `json:"head,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Trace   *Operation `json:"trace,omitempty"`
}

func (p *PathItem) set(method string, op *Operation) error {
	switch method {
	case http.MethodGet:
		p.Get = op
	case http.MethodPut:
		p.Put = op
	case http.MethodPost:
		p.Post = op
	case http.MethodDelete:
		p.Delete = op
	case http.MethodOptions:
		p.Options = op
	case http.MethodHead:
		p.Head = op
	case http.MethodPatch:
		p.Patch = op
	case http.MethodTrace:
		p.Trace = op
	default:
		return fmt.Errorf("routekit: unsupported HTTP method %q", method)
	}
	return nil
}

// Operation is one OpenAPI operation.
type Operation struct {
	ID          string                     `json:"operationId,omitempty"`
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	Tags        []string                   `json:"tags,omitempty"`
	Deprecated  bool                       `json:"deprecated,omitempty"`
	Parameters  []*Parameter               `json:"parameters,omitempty"`
	RequestBody *RequestBodyObject         `json:"requestBody,omitempty"`
	Responses   map[string]*ResponseObject `json:"responses"`
	Security    []map[string][]string      `json:"security,omitempty"`
}

// Parameter is an OpenAPI parameter object.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
	Example     any     `json:"example,omitempty"`
}

// RequestBodyObject is an OpenAPI request body.
type RequestBodyObject struct {
	Description string                `json:"description,omitempty"`
	Required    bool                  `json:"required,omitempty"`
	Content     map[string]*MediaType `json:"content"`
}

// ResponseObject is an OpenAPI response or a reference to one.
type ResponseObject struct {
	Ref         string                `json:"$ref,omitempty"`
	Description string                `json:"description,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"`
}

// MediaType describes the schema and example of one media type.
type MediaType struct {
	Schema  *Schema `json:"schema,omitempty"`
	Example any     `json:"example,omitempty"`
}

// Components holds reusable schemas, responses and security schemes.
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	Responses       map[string]*ResponseObject `json:"responses,omitempty"`
	SecuritySchemes map[string]SecurityScheme  `json:"securitySchemes,omitempty"`
}

// JSON marshals the document with a stable indentation and trailing newline.
func (d *Document) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("routekit: marshal document: %w", err)
	}
	return append(data, '\n'), nil
}
