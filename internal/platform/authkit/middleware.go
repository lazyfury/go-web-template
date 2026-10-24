package authkit

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// Verifier turns a raw access token into a principal. The implementation is
// expected to also load the user (status, token version) so disabled accounts
// and password changes invalidate tokens immediately.
type Verifier interface {
	VerifyAccess(ctx context.Context, rawToken string) (*Principal, error)
}

const principalContextKey = "authkit.principal"

// Authenticate rejects requests without a valid "Authorization: Bearer <jwt>"
// header and stores the principal on the gin context.
func Authenticate(verifier Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c.GetHeader("Authorization"))
		if raw == "" {
			httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "missing bearer token"))
			c.Abort()
			return
		}
		principal, err := verifier.VerifyAccess(c.Request.Context(), raw)
		if err != nil {
			httpx.Fail(c, err)
			c.Abort()
			return
		}
		c.Set(principalContextKey, *principal)
		c.Next()
	}
}

// RequirePermission rejects authenticated callers that lack every listed
// permission (any-of semantics). It must be mounted after Authenticate.
func RequirePermission(permissions ...Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFrom(c)
		if !ok {
			httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "authentication required"))
			c.Abort()
			return
		}
		if !CanAny(principal.Role, permissions...) {
			httpx.Error(c, http.StatusForbidden, apperr.CodeForbidden, "insufficient permissions")
			return
		}
		c.Next()
	}
}

// PrincipalFrom returns the authenticated principal stored by Authenticate.
func PrincipalFrom(c *gin.Context) (Principal, bool) {
	value, ok := c.Get(principalContextKey)
	if !ok {
		return Principal{}, false
	}
	principal, ok := value.(Principal)
	return principal, ok
}

// bearerToken extracts the token from an Authorization header.
func bearerToken(header string) string {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
