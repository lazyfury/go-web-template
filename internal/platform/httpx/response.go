// Package httpx provides a unified JSON envelope for all API responses:
//
//	{ "code": 0, "message": "ok", "data": { ... } }
//
// code == 0 means success; any non-zero code is a business error whose HTTP
// status is derived from the code (see internal/platform/apperr). Error
// responses also carry the X-Request-ID value so clients can correlate them
// with server logs.
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// Body is the unified response envelope.
type Body struct {
	Code      apperr.Code `json:"code"`
	Message   string      `json:"message"`
	Data      any         `json:"data,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

// OK writes a successful response with optional payload.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: apperr.CodeOK, Message: "ok", Data: data})
}

// Created writes a 201 response with the created resource.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{Code: apperr.CodeOK, Message: "created", Data: data})
}

// NoContent writes an empty 204 httpx.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Fail writes an error response from a typed application error, choosing the
// HTTP status from the business code.
func Fail(c *gin.Context, err error) {
	appErr := apperr.From(err)
	c.JSON(appErr.Code.HTTPStatus(), errorBody(c, appErr.Code, appErr.Message))
}

// Error writes an error envelope with an explicit HTTP status. Use it for
// transport-level failures (413, 429, 504) whose status is not derived from
// the business code.
func Error(c *gin.Context, status int, code apperr.Code, message string) {
	c.AbortWithStatusJSON(status, errorBody(c, code, message))
}

func errorBody(c *gin.Context, code apperr.Code, message string) Body {
	return Body{
		Code:      code,
		Message:   message,
		RequestID: RequestIDFromContext(c.Request.Context()),
	}
}
