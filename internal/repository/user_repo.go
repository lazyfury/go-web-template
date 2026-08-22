package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/model"
)

// UserRepository defines the persistence contract for users. The service layer
// depends on this interface, which keeps business logic testable with fakes
// and allows swapping the implementation without touching callers.
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	FindByID(ctx context.Context, id uint64) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	List(ctx context.Context, offset, limit int, keyword string) ([]model.User, int64, error)
	Update(ctx context.Context, user *model.User) error
	Delete(ctx context.Context, id uint64) error
}

// userRepository is the GORM-backed implementation of UserRepository.
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a GORM-backed UserRepository.
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		if isUniqueViolation(err) {
			return apperrors.Wrap(apperrors.CodeConflict, "user already exists", err)
		}
		return apperrors.Wrap(apperrors.CodeDatabase, "create user failed", err)
	}
	return nil
}

func (r *userRepository) FindByID(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrNotFound
	}
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeDatabase, "find user failed", err)
	}
	return &user, nil
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).
		Where("email = ?", strings.ToLower(strings.TrimSpace(email))).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrNotFound
	}
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeDatabase, "find user by email failed", err)
	}
	return &user, nil
}

func (r *userRepository) List(ctx context.Context, offset, limit int, keyword string) ([]model.User, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.User{})
	if keyword != "" {
		// LOWER() keeps the filter portable across PostgreSQL and SQLite.
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(email) LIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, apperrors.Wrap(apperrors.CodeDatabase, "count users failed", err)
	}

	var users []model.User
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&users).Error; err != nil {
		return nil, 0, apperrors.Wrap(apperrors.CodeDatabase, "list users failed", err)
	}
	return users, total, nil
}

func (r *userRepository) Update(ctx context.Context, user *model.User) error {
	if err := r.db.WithContext(ctx).Model(user).Updates(user).Error; err != nil {
		if isUniqueViolation(err) {
			return apperrors.Wrap(apperrors.CodeConflict, "user already exists", err)
		}
		return apperrors.Wrap(apperrors.CodeDatabase, "update user failed", err)
	}
	return nil
}

func (r *userRepository) Delete(ctx context.Context, id uint64) error {
	// Soft delete: gorm.DeletedAt is set, rows are kept for audit.
	res := r.db.WithContext(ctx).Delete(&model.User{}, id)
	if res.Error != nil {
		return apperrors.Wrap(apperrors.CodeDatabase, "delete user failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

// isUniqueViolation detects PostgreSQL unique-constraint violations
// (SQLSTATE 23505) across pgx and lib/pq error shapes.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(fmt.Sprintf("%v", err))
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
}
