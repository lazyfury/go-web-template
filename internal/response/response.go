// Package response provides a unified JSON envelope for all API responses:
//
//	{ "code": 0, "message": "ok", "data": { ... } }
//
// code == 0 means success; any non-zero code is a business error whose HTTP
// status is derived from the code (see internal/errors).
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/example/go-web-template/internal/errors"
)

// Body is the unified response envelope.
type Body struct {
	Code    apperrors.Code `json:"code"`
	Message string         `json:"message"`
	Data    any            `json:"data,omitempty"`
}

// OK writes a successful response with optional payload.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: apperrors.CodeOK, Message: "ok", Data: data})
}

// Created writes a 201 response with the created resource.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{Code: apperrors.CodeOK, Message: "created", Data: data})
}

// NoContent writes an empty 204 response.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Fail writes an error response from a typed application error, choosing the
// HTTP status from the business code.
func Fail(c *gin.Context, err error) {
	appErr := apperrors.From(err)
	c.JSON(appErr.Code.HTTPStatus(), Body{
		Code:    appErr.Code,
		Message: appErr.Message,
	})
}
