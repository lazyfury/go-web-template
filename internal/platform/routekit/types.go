// Package routekit provides declarative route registration on top of gin and
// generates an OpenAPI 3.0 document from the registered metadata.
//
// A route is registered together with an Op value that describes its request,
// responses and error codes. The registry builds the specification once at
// startup, so the document and the actual routes cannot drift apart.
package routekit

import (
	"reflect"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// BearerAuth is the security-scheme name used for JWT bearer authentication.
// It is referenced by Config.SecuritySchemes and by Op.Security, so the
// document and the runtime guard share one string.
const BearerAuth = "bearerAuth"

// BearerScheme returns the OpenAPI security scheme for HTTP bearer JWTs.
func BearerScheme() SecurityScheme {
	return SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Access token issued by POST /api/v1/auth/login",
	}
}

// ParamIn is the location of a parameter in an OpenAPI operation.
type ParamIn string

const (
	// InPath marks a path parameter. Path parameters are always required.
	InPath ParamIn = "path"
	// InQuery marks a query parameter.
	InQuery ParamIn = "query"
	// InHeader marks a request header parameter.
	InHeader ParamIn = "header"
	// InCookie marks a cookie parameter.
	InCookie ParamIn = "cookie"
)

// Info is the OpenAPI info object.
type Info struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

// Server is an OpenAPI server entry.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// SecurityScheme describes an OpenAPI security scheme. Only the fields needed
// by the common HTTP bearer and API-key cases are modeled.
type SecurityScheme struct {
	Type         string `json:"type"`
	Description  string `json:"description,omitempty"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	In           string `json:"in,omitempty"`
	Name         string `json:"name,omitempty"`
}

// Config controls the generated document and the interpretation of metadata.
type Config struct {
	// Info is the document info block. Title defaults to "API" and Version to
	// "1.0.0" when empty.
	Info Info
	// BasePath is stripped from registered gin paths so operations are
	// relative to the server URL. For example "/api/v1".
	BasePath string
	// Servers are emitted as the OpenAPI servers array.
	Servers []Server
	// Tags maps tag names to descriptions for the top-level tags array.
	Tags map[string]string
	// SecuritySchemes are emitted under components.securitySchemes.
	SecuritySchemes map[string]SecurityScheme
	// EnvelopeType is the struct used as the response envelope. The field with
	// the JSON name "data" is replaced by each response's data type. It
	// defaults to httpx.Body.
	EnvelopeType reflect.Type
	// DefaultErrors are added to every operation unless SkipDefaultErrors is
	// set. Defaults to 413, 429 and 500.
	DefaultErrors []apperr.Code
}

// Op is the declarative description of one route.
type Op struct {
	// ID is the OpenAPI operationId. It defaults to method + camel-cased path,
	// for example "getUsersId", and must be unique.
	ID          string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	// Security lists security scheme names defined in Config.SecuritySchemes.
	Security []string
	// SkipDefaultErrors disables the automatic 413/429/500 responses.
	SkipDefaultErrors bool

	Params    []Param
	Request   *RequestBody
	Responses []Response
	Errors    []apperr.Code
}

// Param describes one path, query, header or cookie parameter.
type Param struct {
	Name        string
	In          ParamIn
	Type        reflect.Type
	Required    bool
	Description string
	Example     any
}

// RequestBody describes an operation request body.
type RequestBody struct {
	Type        reflect.Type
	Required    bool
	ContentType string
	Description string
	Example     any
}

// Response describes one response. Data may be nil for responses without a
// body; Envelope wraps Data in the configured response envelope.
type Response struct {
	Status      int
	Data        reflect.Type
	Envelope    bool
	Description string
}

// OperationInfo is the public, read-only view of a registered operation. It is
// used by completeness tests that compare gin's route table with the registry.
type OperationInfo struct {
	Method   string
	Path     string
	SpecPath string
	ID       string
}

// JSON builds a JSON request body for T.
func JSON[T any](required bool) *RequestBody {
	return &RequestBody{Type: reflect.TypeFor[T](), Required: required}
}

// Path builds a required path parameter of type T.
func Path[T any](name, description string) Param {
	return Param{
		Name:        name,
		In:          InPath,
		Type:        reflect.TypeFor[T](),
		Required:    true,
		Description: description,
	}
}

// Query builds a query parameter of type T.
func Query[T any](name, description string, required bool) Param {
	return Param{
		Name:        name,
		In:          InQuery,
		Type:        reflect.TypeFor[T](),
		Required:    required,
		Description: description,
	}
}

// Header builds a header parameter of type T.
func Header[T any](name, description string, required bool) Param {
	return Param{
		Name:        name,
		In:          InHeader,
		Type:        reflect.TypeFor[T](),
		Required:    required,
		Description: description,
	}
}

// OK describes a 200 response wrapped in the detected envelope.
func OK[T any](description string) Response {
	return Response{Status: 200, Data: reflect.TypeFor[T](), Envelope: true, Description: description}
}

// Created describes a 201 response wrapped in the detected envelope.
func Created[T any](description string) Response {
	return Response{Status: 201, Data: reflect.TypeFor[T](), Envelope: true, Description: description}
}

// NoContent describes a 204 response without a body.
func NoContent() Response {
	return Response{Status: 204}
}
