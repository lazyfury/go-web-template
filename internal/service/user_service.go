// Package service contains the business-logic layer. Services depend on
// repository interfaces and are framework-agnostic (no gin imports), which
// keeps them unit-testable and reusable across transports.
package service

import (
	"context"
	"errors"
	"strings"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/model"
	"github.com/example/go-web-template/internal/repository"
)

// UserInput is the validated input used to create a user.
type UserInput struct {
	Name  string
	Email string
}

// UserPatch is the validated input used to update a user. Pointer fields
// distinguish "not provided" from "zero value".
type UserPatch struct {
	Name  *string
	Email *string
}

// UserService exposes user use-cases.
type UserService interface {
	Create(ctx context.Context, in UserInput) (*model.User, error)
	Get(ctx context.Context, id uint64) (*model.User, error)
	List(ctx context.Context, page, pageSize int, keyword string) ([]model.User, int64, error)
	Update(ctx context.Context, id uint64, patch UserPatch) (*model.User, error)
	Delete(ctx context.Context, id uint64) error
}

type userService struct {
	repo repository.UserRepository
}

// NewUserService creates a UserService backed by the given repository.
func NewUserService(repo repository.UserRepository) UserService {
	return &userService{repo: repo}
}

func (s *userService) Create(ctx context.Context, in UserInput) (*model.User, error) {
	email := normalizeEmail(in.Email)
	if _, err := s.repo.FindByEmail(ctx, email); err == nil {
		return nil, apperrors.Wrap(apperrors.CodeConflict, "email already registered", nil)
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return nil, err
	}

	user := &model.User{
		Name:   strings.TrimSpace(in.Name),
		Email:  email,
		Status: model.UserStatusActive,
	}
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *userService) Get(ctx context.Context, id uint64) (*model.User, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *userService) List(ctx context.Context, page, pageSize int, keyword string) ([]model.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, offset, pageSize, strings.TrimSpace(keyword))
}

func (s *userService) Update(ctx context.Context, id uint64, patch UserPatch) (*model.User, error) {
	user, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if patch.Email != nil {
		email := normalizeEmail(*patch.Email)
		if existing, err := s.repo.FindByEmail(ctx, email); err == nil && existing.ID != id {
			return nil, apperrors.Wrap(apperrors.CodeConflict, "email already registered", nil)
		} else if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
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

func (s *userService) Delete(ctx context.Context, id uint64) error {
	return s.repo.Delete(ctx, id)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
