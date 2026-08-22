package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/response"
)

// Recovery converts panics into 500 responses and logs the stack trace.
// It must be the outermost middleware so that aborted responses still reach
// the client.
func Recovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error("panic recovered",
					zap.Any("error", err),
					zap.ByteString("stack", debug.Stack()),
					zap.String("request_id", RequestIDFromContext(c.Request.Context())),
				)
				// Avoid a duplicate header/status write if the handler already
				// started writing.
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, response.Body{
						Code:    apperrors.CodeInternal,
						Message: "internal server error",
					})
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}
