// Package apperr defines typed application errors with stable business codes
// that are returned to API clients inside the unified response envelope.
//
// Code layout: [HTTP status][category][sequence]
//
//	e.g. 40400 = HTTP 404, category 0 (generic), sequence 0.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable business error code exposed through the API.
type Code int

const (
	// CodeOK signals a successful httpx.
	CodeOK Code = 0
	// CodeInvalidArgument signals a client-side validation failure.
	CodeInvalidArgument Code = 40000
	// CodeUnauthorized signals a missing or invalid credential.
	CodeUnauthorized Code = 40100
	// CodeForbidden signals insufficient permissions.
	CodeForbidden Code = 40300
	// CodeNotFound signals a missing resource.
	CodeNotFound Code = 40400
	// CodeConflict signals a state conflict (e.g. duplicate email).
	CodeConflict Code = 40900
	// CodePayloadTooLarge signals a request body above the configured limit.
	CodePayloadTooLarge Code = 41300
	// CodeTooManyRequests signals rate limiting.
	CodeTooManyRequests Code = 42900
	// CodeInternal signals an unexpected server error.
	CodeInternal Code = 50000
	// CodeDatabase signals a database-layer failure.
	CodeDatabase Code = 50001
)

// Error is an application error carrying a business code.
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap exposes the wrapped cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Is makes errors.Is(err, target) match on business code, so callers can
// compare against package-level sentinels (e.g. ErrNotFound) even when the
// error was produced by Wrap with a different message.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// New creates an application error with a business code.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap wraps an underlying error while assigning a business code.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// From maps an arbitrary error to a typed *Error. Unknown errors become
// CodeInternal so callers never leak internals to clients.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return &Error{Code: CodeInternal, Message: "internal server error", Cause: err}
}

// HTTPStatus maps a business code to the HTTP status code. Known codes are
// matched exactly; unknown codes fall back to the numeric layout
// [HTTP status][category][sequence] so custom codes still map sensibly.
func (c Code) HTTPStatus() int {
	switch c {
	case CodeOK:
		return http.StatusOK
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeTooManyRequests:
		return http.StatusTooManyRequests
	case CodeInternal, CodeDatabase:
		return http.StatusInternalServerError
	}

	switch {
	case c >= 50000:
		return http.StatusInternalServerError
	case c >= 42900:
		return http.StatusTooManyRequests
	case c >= 40900:
		return http.StatusConflict
	case c >= 40400:
		return http.StatusNotFound
	case c >= 40300:
		return http.StatusForbidden
	case c >= 40100:
		return http.StatusUnauthorized
	case c >= 40000:
		return http.StatusBadRequest
	default:
		return http.StatusOK
	}
}

// Helpers for the common cases.
var (
	ErrNotFound        = New(CodeNotFound, "resource not found")
	ErrInvalidArgument = New(CodeInvalidArgument, "invalid argument")
	ErrConflict        = New(CodeConflict, "resource conflict")
	ErrUnauthorized    = New(CodeUnauthorized, "unauthorized")
	ErrForbidden       = New(CodeForbidden, "forbidden")
)
