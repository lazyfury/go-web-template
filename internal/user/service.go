package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
)

// Input is the validated input used to create a user (admin provisioning).
type Input struct {
	Name     string
	Email    string
	Password string
	Role     authkit.Role
}

// Patch is the validated input used to update a user. Pointer fields
// distinguish "not provided" from "zero value".
type Patch struct {
	Name     *string
	Email    *string
	Role     *authkit.Role
	Password *string
	Status   *Status
}

// SessionRevoker revokes every refresh session of a user. It is satisfied by
// auth.Repository; defining it here keeps the dependency one-way (user does not
// import auth) and lets tests pass a fake.
type SessionRevoker interface {
	RevokeAllForUser(ctx context.Context, userID uint64) error
}

// Service implements the user use-cases. It depends on the Repository
// interface only, so business rules are unit-testable without a database.
type Service struct {
	repo    Repository
	hasher  *passwd.Hasher
	revoker SessionRevoker
	now     func() time.Time
}

// NewService creates a Service backed by the given repository and session
// revoker (nil disables revocation, which is only useful in narrow tests).
func NewService(repo Repository, hasher *passwd.Hasher, revoker SessionRevoker) *Service {
	return &Service{repo: repo, hasher: hasher, revoker: revoker, now: time.Now}
}

// Create provisions a user with credentials and rejects duplicate emails.
// Authorization (users:write) is enforced by the route middleware.
func (s *Service) Create(ctx context.Context, in Input) (*User, error) {
	email := normalizeEmail(in.Email)
	if _, err := s.repo.FindByEmail(ctx, email); err == nil {
		return nil, apperr.Wrap(apperr.CodeConflict, "email already registered", nil)
	} else if !errors.Is(err, apperr.ErrNotFound) {
		return nil, err
	}

	role := in.Role
	if role == "" {
		role = authkit.RoleMember
	}
	if !role.Valid() {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, "invalid role", nil)
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, err.Error(), err)
	}

	now := s.now()
	user := &User{
		Name:              strings.TrimSpace(in.Name),
		Email:             email,
		Status:            StatusActive,
		Role:              role,
		PasswordHash:      hash,
		PasswordChangedAt: &now,
	}
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// Get returns a user by id. Members may only read themselves.
func (s *Service) Get(ctx context.Context, actor authkit.Principal, id uint64) (*User, error) {
	if !canRead(actor, id) {
		return nil, forbidden("insufficient permissions")
	}
	return s.repo.FindByID(ctx, id)
}

// List returns a page of users matching an optional keyword. Admin only.
func (s *Service) List(ctx context.Context, actor authkit.Principal, page, pageSize int, keyword string) ([]User, int64, error) {
	if !actor.IsAdmin() {
		return nil, 0, forbidden("insufficient permissions")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, offset, pageSize, strings.TrimSpace(keyword))
}

// Update applies a partial update, rejecting an email owned by another user.
// Members may only update themselves and may not change role, status or
// password through this endpoint (password changes go through /auth/password).
func (s *Service) Update(ctx context.Context, actor authkit.Principal, id uint64, patch Patch) (*User, error) {
	if !canRead(actor, id) {
		return nil, forbidden("insufficient permissions")
	}
	adminOnlyChange := patch.Role != nil || patch.Status != nil || patch.Password != nil
	if adminOnlyChange && !actor.IsAdmin() {
		return nil, forbidden("insufficient permissions")
	}

	user, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if patch.Email != nil {
		email := normalizeEmail(*patch.Email)
		if existing, err := s.repo.FindByEmail(ctx, email); err == nil && existing.ID != id {
			return nil, apperr.Wrap(apperr.CodeConflict, "email already registered", nil)
		} else if err != nil && !errors.Is(err, apperr.ErrNotFound) {
			return nil, err
		}
		user.Email = email
	}
	if patch.Name != nil {
		user.Name = strings.TrimSpace(*patch.Name)
	}

	authStateChanged := false
	if patch.Role != nil {
		if !patch.Role.Valid() {
			return nil, apperr.Wrap(apperr.CodeInvalidArgument, "invalid role", nil)
		}
		user.Role = *patch.Role
		authStateChanged = true
	}
	if patch.Password != nil {
		hash, err := s.hasher.Hash(*patch.Password)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInvalidArgument, err.Error(), err)
		}
		now := s.now()
		user.PasswordHash = hash
		user.PasswordChangedAt = &now
		user.TokenVersion++
		authStateChanged = true
	}
	if patch.Status != nil {
		if *patch.Status != StatusActive && *patch.Status != StatusDisabled {
			return nil, apperr.Wrap(apperr.CodeInvalidArgument, "invalid status", nil)
		}
		if *patch.Status != user.Status {
			user.Status = *patch.Status
			// Disabling must revoke outstanding access tokens immediately.
			user.TokenVersion++
			authStateChanged = true
		}
	}

	if authStateChanged {
		// SaveAuthState also writes the profile columns, so a single call keeps
		// name/email/role/status consistent.
		if err := s.repo.SaveAuthState(ctx, user); err != nil {
			return nil, err
		}
		// A credential reset or a disable must also kill refresh sessions:
		// otherwise a stolen refresh token could mint tokens after the change.
		credentialsOrStatusChanged := patch.Password != nil || patch.Status != nil
		if credentialsOrStatusChanged && s.revoker != nil {
			if err := s.revoker.RevokeAllForUser(ctx, user.ID); err != nil {
				return nil, err
			}
		}
	} else if err := s.repo.Update(ctx, user); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, id)
}

// Delete soft-deletes a user. Admin only, and admins cannot delete themselves.
func (s *Service) Delete(ctx context.Context, actor authkit.Principal, id uint64) error {
	if !actor.IsAdmin() {
		return forbidden("insufficient permissions")
	}
	if actor.UserID == id {
		return forbidden("cannot delete your own account")
	}
	return s.repo.Delete(ctx, id)
}

// canRead reports whether actor may access the user row id.
func canRead(actor authkit.Principal, id uint64) bool {
	return actor.IsAdmin() || actor.UserID == id
}

func forbidden(message string) error {
	return apperr.New(apperr.CodeForbidden, message)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
