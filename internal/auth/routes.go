package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// RegisterRoutes mounts the public auth endpoints (login, refresh). The
// optional loginGuards are applied to /login only: a brute-force limiter must
// not throttle refresh traffic.
func RegisterRoutes(rg *routekit.Group, h *Handler, loginGuards ...gin.HandlerFunc) {
	rg.Registry().DescribeTag("auth", "Authentication: password login, JWT access token, refresh-token rotation")
	authGroup := rg.Group("/auth", routekit.Tags("auth"))

	login := authGroup.Group("", routekit.Middleware(loginGuards...))
	login.POST("/login", &routekit.Op{
		ID:      "login",
		Summary: "Exchange email and password for a JWT access token",
		Description: "Sets an HttpOnly refresh cookie and returns the access token. " +
			"Failures are deliberately uniform (401 invalid credentials) to avoid user enumeration.",
		Request: routekit.JSON[LoginRequest](true),
		Params: []routekit.Param{
			routekit.Header[string](RefreshTransportHeader, `set to "body" for non-browser clients (refresh token in the response body instead of a cookie)`, false),
		},
		Responses: []routekit.Response{routekit.OK[LoginResponse]("authenticated")},
		// No 403: locked and disabled accounts answer like invalid credentials
		// (401) so the endpoint cannot be used to enumerate addresses.
		Errors: []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeUnauthorized},
	}, h.Login)

	authGroup.POST("/refresh", &routekit.Op{
		ID:      "refreshToken",
		Summary: "Rotate the refresh cookie and issue a new access token",
		Description: "The presented refresh token is revoked and replaced. Token rotation is slid: an actively used session never has to sign in again, up to auth.refresh_max_lifetime. " +
			"A token replayed within auth.refresh_reuse_interval is tolerated (multi-tab race); outside that window it revokes every session. " +
			"Browsers use the HttpOnly cookie; non-browser clients may send X-Refresh-Token and receive the rotated token in the body.",
		Params: []routekit.Param{
			routekit.Header[string](RefreshTokenHeader, "refresh token for non-browser clients (cookie alternative)", false),
		},
		Responses: []routekit.Response{routekit.OK[LoginResponse]("rotated session")},
		Errors:    []apperr.Code{apperr.CodeUnauthorized, apperr.CodeForbidden},
	}, h.Refresh)
}

// RegisterProtectedRoutes mounts the endpoints that require a valid access
// token. It must be called on the authenticated group.
func RegisterProtectedRoutes(rg *routekit.Group, h *Handler, passwordGuards ...gin.HandlerFunc) {
	authGroup := rg.Group("/auth", routekit.Tags("auth"), routekit.Security(routekit.BearerAuth))

	authGroup.POST("/logout", &routekit.Op{
		ID:      "logout",
		Summary: "Revoke the current refresh session (immediate, never tolerated inside the reuse interval)",
		Params: []routekit.Param{
			routekit.Header[string](RefreshTokenHeader, "refresh token for non-browser clients (cookie alternative)", false),
		},
		Responses: []routekit.Response{routekit.NoContent()},
	}, h.Logout)

	authGroup.GET("/me", &routekit.Op{
		ID:        "getCurrentUser",
		Summary:   "Return the authenticated user, role and permissions",
		Responses: []routekit.Response{routekit.OK[UserView]("current user")},
		Errors:    []apperr.Code{apperr.CodeNotFound},
	}, h.Me)

	// Changing the password runs bcrypt on the current password, so it needs its
	// own per-user budget: without one, a stolen access token can both grind
	// passwords online and burn CPU at full speed (the global IP limiter is
	// useless to an attacker who can rotate IPs).
	passwordGroup := authGroup.Group("", routekit.Middleware(passwordGuards...))
	passwordGroup.PATCH("/password", &routekit.Op{
		ID:      "changePassword",
		Summary: "Change the caller's password",
		Description: "Rotates the bcrypt hash, bumps the token version (all access tokens become invalid) and revokes every refresh session. " +
			"Rate limited per user (auth.password_rps / auth.password_burst).",
		Request:   routekit.JSON[ChangePasswordRequest](true),
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeUnauthorized, apperr.CodeTooManyRequests},
	}, h.ChangePassword)
}
