package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
	"github.com/example/go-web-template/internal/user"
)

// RefreshCookieName is the HttpOnly cookie carrying the refresh token. The
// access token never touches cookies: it stays in the SPA's memory.
const RefreshCookieName = "gw_refresh"

// refreshCookiePath scopes the cookie to the auth endpoints.
const refreshCookiePath = "/api/v1/auth"

// Handler exposes the auth use-cases over HTTP.
type Handler struct {
	svc          *Service
	cookieSecure bool
	refreshTTL   time.Duration
}

// NewHandler creates a Handler.
func NewHandler(svc *Service, cookieSecure bool, refreshTTL time.Duration) *Handler {
	return &Handler{svc: svc, cookieSecure: cookieSecure, refreshTTL: refreshTTL}
}

// Login handles POST /api/v1/auth/login.
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, err.Error(), err))
		return
	}
	session, err := h.svc.Login(c.Request.Context(), LoginInput{
		Email:    req.Email,
		Password: req.Password,
		ClientMeta: ClientMeta{
			UserAgent: c.Request.UserAgent(),
			IP:        c.ClientIP(),
		},
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.setRefreshCookie(c, session)
	httpx.OK(c, loginResponse(session))
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *Handler) Refresh(c *gin.Context) {
	raw, err := c.Cookie(RefreshCookieName)
	if err != nil || raw == "" {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "missing refresh token"))
		return
	}
	session, err := h.svc.Refresh(c.Request.Context(), raw, ClientMeta{
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
	})
	if err != nil {
		h.clearRefreshCookie(c)
		httpx.Fail(c, err)
		return
	}
	h.setRefreshCookie(c, session)
	httpx.OK(c, loginResponse(session))
}

// Logout handles POST /api/v1/auth/logout.
func (h *Handler) Logout(c *gin.Context) {
	if raw, err := c.Cookie(RefreshCookieName); err == nil && raw != "" {
		if err := h.svc.Logout(c.Request.Context(), raw); err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	h.clearRefreshCookie(c)
	httpx.NoContent(c)
}

// Me handles GET /api/v1/auth/me.
func (h *Handler) Me(c *gin.Context) {
	principal, ok := authkit.PrincipalFrom(c)
	if !ok {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "authentication required"))
		return
	}
	account, err := h.svc.Me(c.Request.Context(), principal.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, userView(account))
}

// ChangePassword handles PATCH /api/v1/auth/password.
func (h *Handler) ChangePassword(c *gin.Context) {
	principal, ok := authkit.PrincipalFrom(c)
	if !ok {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "authentication required"))
		return
	}
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, err.Error(), err))
		return
	}
	if err := h.svc.ChangePassword(c.Request.Context(), principal.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		httpx.Fail(c, err)
		return
	}
	// Every session was revoked, including the current refresh token.
	h.clearRefreshCookie(c)
	httpx.NoContent(c)
}

func (h *Handler) setRefreshCookie(c *gin.Context, session *Session) {
	maxAge := int(h.refreshTTL.Seconds())
	if maxAge <= 0 {
		maxAge = int(time.Until(session.RefreshExpiresAt).Seconds())
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(RefreshCookieName, session.RefreshToken, maxAge, refreshCookiePath, "", h.cookieSecure, true)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(RefreshCookieName, "", -1, refreshCookiePath, "", h.cookieSecure, true)
}

func loginResponse(session *Session) LoginResponse {
	return LoginResponse{
		AccessToken: session.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   session.AccessExpiresIn,
		User:        userView(session.User),
	}
}

func userView(account *user.User) UserView {
	return UserView{
		ID:          account.ID,
		Name:        account.Name,
		Email:       account.Email,
		Role:        account.Role,
		Permissions: authkit.Permissions(account.Role),
	}
}
