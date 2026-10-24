package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// Repository is the persistence contract for refresh-token sessions.
type Repository interface {
	Create(ctx context.Context, token *RefreshToken) error
	FindByHash(ctx context.Context, hash string) (*RefreshToken, error)
	// RevokeIfActive revokes a token and reports whether it transitioned from
	// active to revoked. false means another request already used it, which is
	// treated as refresh-token reuse.
	RevokeIfActive(ctx context.Context, id uint64) (bool, error)
	RevokeAllForUser(ctx context.Context, userID uint64) error
}

// gormRepository is the GORM-backed implementation of Repository.
type gormRepository struct {
	db *gorm.DB
}

// NewRepository creates a GORM-backed Repository.
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(ctx context.Context, token *RefreshToken) error {
	if err := r.db.WithContext(ctx).Create(token).Error; err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "create refresh token failed", err)
	}
	return nil
}

func (r *gormRepository) FindByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	var token RefreshToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ?", strings.ToLower(hash)).
		First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find refresh token failed", err)
	}
	return &token, nil
}

func (r *gormRepository) RevokeIfActive(ctx context.Context, id uint64) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", now)
	if res.Error != nil {
		return false, apperr.Wrap(apperr.CodeDatabase, "revoke refresh token failed", res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *gormRepository) RevokeAllForUser(ctx context.Context, userID uint64) error {
	now := time.Now()
	err := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "revoke refresh tokens failed", err)
	}
	return nil
}
