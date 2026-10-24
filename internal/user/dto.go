package user

import (
	"time"

	"github.com/example/go-web-template/internal/platform/authkit"
)

// CreateRequest is the payload for POST /users. Accounts are provisioned by an
// admin, so a password is required (there is no public registration flow).
type CreateRequest struct {
	Name     string       `json:"name" binding:"required,min=2,max=64"`
	Email    string       `json:"email" binding:"required,email,max=255"`
	Password string       `json:"password" binding:"required,min=8,max=72"`
	Role     authkit.Role `json:"role" binding:"omitempty,oneof=admin member"`
}

// UpdateRequest is the payload for PATCH /users/:id.
type UpdateRequest struct {
	Name     *string       `json:"name" binding:"omitempty,min=2,max=64"`
	Email    *string       `json:"email" binding:"omitempty,email,max=255"`
	Role     *authkit.Role `json:"role" binding:"omitempty,oneof=admin member"`
	Password *string       `json:"password" binding:"omitempty,min=8,max=72"`
	Status   *Status       `json:"status" binding:"omitempty,oneof=0 1"`
}

// ListResponse is the payload for GET /users.
type ListResponse struct {
	Items    []UserDTO `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// UserDTO is the HTTP representation of a user. It is intentionally separate
// from the GORM entity so persistence-only fields (password hash, lock state,
// deleted_at) never leak into the API or the OpenAPI schema.
//
//nolint:revive // user.UserDTO is explicit at call sites and keeps the OpenAPI component name stable.
type UserDTO struct {
	ID        uint64       `json:"id"`
	Name      string       `json:"name"`
	Email     string       `json:"email"`
	Status    Status       `json:"status" enum:"0,1"`
	Role      authkit.Role `json:"role" enum:"admin,member"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func toDTO(user *User) UserDTO {
	return UserDTO{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		Status:    user.Status,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}

func toDTOs(users []User) []UserDTO {
	out := make([]UserDTO, 0, len(users))
	for i := range users {
		out = append(out, toDTO(&users[i]))
	}
	return out
}
