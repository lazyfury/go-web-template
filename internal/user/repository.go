package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// Repository defines the persistence contract for users. The service layer
// depends on this interface, which keeps business logic testable with fakes
// and allows swapping the implementation without touching callers.
type Repository interface {
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id uint64) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, offset, limit int, keyword string) ([]User, int64, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id uint64) error
}

// gormRepository is the GORM-backed implementation of Repository.
type gormRepository struct {
	db *gorm.DB
}

// NewRepository creates a GORM-backed Repository.
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(ctx context.Context, user *User) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "user already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "create user failed", err)
	}
	return nil
}

func (r *gormRepository) FindByID(ctx context.Context, id uint64) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find user failed", err)
	}
	return &user, nil
}

func (r *gormRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).
		Where("email = ?", strings.ToLower(strings.TrimSpace(email))).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find user by email failed", err)
	}
	return &user, nil
}

func (r *gormRepository) List(ctx context.Context, offset, limit int, keyword string) ([]User, int64, error) {
	query := r.db.WithContext(ctx).Model(&User{})
	if keyword != "" {
		// LOWER() keeps the filter portable across PostgreSQL and SQLite.
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(email) LIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count users failed", err)
	}

	var users []User
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&users).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list users failed", err)
	}
	return users, total, nil
}

func (r *gormRepository) Update(ctx context.Context, user *User) error {
	if err := r.db.WithContext(ctx).Model(user).Updates(user).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "user already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "update user failed", err)
	}
	return nil
}

func (r *gormRepository) Delete(ctx context.Context, id uint64) error {
	// Soft delete: gorm.DeletedAt is set, rows are kept for audit.
	res := r.db.WithContext(ctx).Delete(&User{}, id)
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "delete user failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
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
