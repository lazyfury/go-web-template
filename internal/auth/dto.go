package auth

import "github.com/example/go-web-template/internal/platform/authkit"

// LoginRequest is the payload for POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

// ChangePasswordRequest is the payload for PATCH /auth/password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required,min=8,max=72"`
	NewPassword     string `json:"new_password" binding:"required,min=8,max=72"`
}

// UserView is the authenticated user as exposed to clients.
//
//nolint:revive // auth.UserView is explicit at call sites and keeps the OpenAPI component name stable.
type UserView struct {
	ID          uint64               `json:"id"`
	Name        string               `json:"name"`
	Email       string               `json:"email"`
	Role        authkit.Role         `json:"role" enum:"admin,member"`
	Permissions []authkit.Permission `json:"permissions"`
}

// LoginResponse is returned by login and refresh. The refresh token itself is
// delivered as an HttpOnly cookie, never in the body.
type LoginResponse struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type"`
	ExpiresIn   int64    `json:"expires_in"`
	User        UserView `json:"user"`
}
