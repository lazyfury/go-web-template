package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
)

// Input is the validated input used to create a user (admin provisioning).
type Input struct {
	Name     string
	Email    string
	Phone    string
	Password string
	Role     authkit.Role
}

// Patch is the validated input used to update a user. Pointer fields
// distinguish "not provided" from "zero value".
type Patch struct {
	Name     *string
	Email    *string
	Phone    *string
	Role     *authkit.Role
	Password *string
	Status   *Status
}

// SessionRevoker revokes every refresh session of a user. The composition root
// adapts auth.Repository to this one-method interface, keeping the dependency
// one-way (user does not import auth) and the tests trivial.
type SessionRevoker interface {
	RevokeUserSessions(ctx context.Context, userID uint64) error
}

// AuthStateInvalidator drops any cached authorization state (status, role,
// token version) for a user, so the change is visible on the next request
// instead of after the cache TTL. A nil implementation means nothing is cached.
//
// It exists separately from SessionRevoker because the two have different
// triggers: a role change invalidates cached state but kills no session, while
// a delete kills both.
type AuthStateInvalidator interface {
	InvalidateAuthState(userID uint64)
}

// Service implements the user use-cases. It depends on the Repository
// interface only, so business rules are unit-testable without a database.
type Service struct {
	repo        Repository
	hasher      *passwd.Hasher
	revoker     SessionRevoker
	invalidator AuthStateInvalidator
	now         func() time.Time
}

// NewService creates a Service backed by the given repository, session revoker
// and auth-state invalidator (nil disables revocation/invalidation, which is
// only useful in narrow tests).
func NewService(repo Repository, hasher *passwd.Hasher, revoker SessionRevoker, invalidator AuthStateInvalidator) *Service {
	return &Service{repo: repo, hasher: hasher, revoker: revoker, invalidator: invalidator, now: time.Now}
}

// invalidateAuthState forgets cached authorization state after a write that can
// change it. Call it even when the process did not change the column itself:
// a role change still makes the cached role wrong.
func (s *Service) invalidateAuthState(userID uint64) {
	if s.invalidator != nil {
		s.invalidator.InvalidateAuthState(userID)
	}
}

// Create provisions a user with credentials and rejects duplicate emails.
// Authorization (users:write) is enforced by the route middleware.
func (s *Service) Create(ctx context.Context, in *Input) (*User, error) {
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

	phone := normalizePhone(in.Phone)
	if utf8.RuneCountInString(phone) > 32 {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, "phone must be at most 32 characters", nil)
	}

	now := s.now()
	user := &User{
		Name:              strings.TrimSpace(in.Name),
		Email:             email,
		Phone:             phone,
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
// It also returns the normalized page/pageSize so the handler echoes what was
// actually applied: a request for page_size=100000 used to come back with
// page_size=100000 in the envelope while only 20 rows were read.
func (s *Service) List(ctx context.Context, actor authkit.Principal, page, pageSize int, keyword string) (items []User, total int64, normalizedPage, normalizedSize int, err error) {
	if !actor.IsAdmin() {
		return nil, 0, 0, 0, forbidden("insufficient permissions")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	items, total, err = s.repo.List(ctx, offset, pageSize, strings.TrimSpace(keyword))
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return items, total, page, pageSize, nil
}

// Update applies a partial update, rejecting an email owned by another user.
// Members may only update their own name/phone: role, status, password and
// email are admin-only (email also because the conflict error would let any
// member probe which addresses are registered).
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
		// A member may not change their own email: the "email already
		// registered" conflict is an enumeration oracle (any authenticated
		// member could test whether an address has an account). Email changes
		// are an admin operation until there is a verified self-service flow.
		if !actor.IsAdmin() {
			return nil, forbidden("insufficient permissions")
		}
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
	if patch.Phone != nil {
		phone := normalizePhone(*patch.Phone)
		if utf8.RuneCountInString(phone) > 32 {
			return nil, apperr.Wrap(apperr.CodeInvalidArgument, "phone must be at most 32 characters", nil)
		}
		user.Phone = phone
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
			if err := s.revoker.RevokeUserSessions(ctx, user.ID); err != nil {
				return nil, err
			}
		}
	} else if err := s.repo.Update(ctx, user); err != nil {
		return nil, err
	}
	// A profile-only edit leaves the cached authorization state correct, but
	// invalidating unconditionally keeps this call site impossible to get wrong.
	s.invalidateAuthState(user.ID)
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
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	// The row is gone: a cached entry would keep the account authenticated for
	// up to the cache TTL.
	s.invalidateAuthState(id)
	return nil
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

func normalizePhone(phone string) string {
	return strings.TrimSpace(phone)
}
