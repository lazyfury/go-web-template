package user

import (
	"context"
	"errors"
	"strings"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// Input is the validated input used to create a user.
type Input struct {
	Name  string
	Email string
}

// Patch is the validated input used to update a user. Pointer fields
// distinguish "not provided" from "zero value".
type Patch struct {
	Name  *string
	Email *string
}

// Service implements the user use-cases. It depends on the Repository
// interface only, so business rules are unit-testable without a database.
type Service struct {
	repo Repository
}

// NewService creates a Service backed by the given repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create registers a user after normalising the email and rejecting duplicates.
func (s *Service) Create(ctx context.Context, in Input) (*User, error) {
	email := normalizeEmail(in.Email)
	if _, err := s.repo.FindByEmail(ctx, email); err == nil {
		return nil, apperr.Wrap(apperr.CodeConflict, "email already registered", nil)
	} else if !errors.Is(err, apperr.ErrNotFound) {
		return nil, err
	}

	user := &User{
		Name:   strings.TrimSpace(in.Name),
		Email:  email,
		Status: StatusActive,
	}
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// Get returns a user by id.
func (s *Service) Get(ctx context.Context, id uint64) (*User, error) {
	return s.repo.FindByID(ctx, id)
}

// List returns a page of users matching an optional keyword.
func (s *Service) List(ctx context.Context, page, pageSize int, keyword string) ([]User, int64, error) {
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
func (s *Service) Update(ctx context.Context, id uint64, patch Patch) (*User, error) {
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

	if err := s.repo.Update(ctx, user); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, id)
}

// Delete soft-deletes a user.
func (s *Service) Delete(ctx context.Context, id uint64) error {
	return s.repo.Delete(ctx, id)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
