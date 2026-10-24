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
	// RevokeIfActive revokes a token at the given instant and reports whether it
	// transitioned from active to revoked. false means another request already
	// used it, which is handled by the service's reuse interval.
	RevokeIfActive(ctx context.Context, id uint64, revokedAt time.Time, reason string) (bool, error)
	RevokeAllForUser(ctx context.Context, userID uint64, revokedAt time.Time, reason string) error
	// RevokeOthersForUser revokes every active token of userID except keepID.
	// It is used when a replayed token is tolerated inside the reuse window:
	// all the racing tabs share one cookie jar, so only the newest token should
	// survive instead of accumulating orphaned sessions.
	RevokeOthersForUser(ctx context.Context, userID uint64, keepID uint64, revokedAt time.Time, reason string) error
	// RetireRotatedForUser rewrites the reason of already-rotated tokens so the
	// reuse grace window can no longer tolerate them. Rotated rows are the only
	// ones a replay may resurrect; when a hard revocation (logout, password
	// change, reuse detection, session cap) happens, they must stop being
	// replayable immediately — otherwise a token rotated seconds before the
	// logout would still mint a fresh session afterwards.
	RetireRotatedForUser(ctx context.Context, userID uint64, reason string) error
	// PurgeExpired hard-deletes tokens that expired before the cutoff. Expiry is
	// already enforced by Active(), so these rows can no longer mint a session;
	// keeping them only grows the table and the digest list. The housekeeping
	// worker calls it so the retention policy stays out of the service layer.
	PurgeExpired(ctx context.Context, before time.Time) (int64, error)
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

func (r *gormRepository) RevokeIfActive(ctx context.Context, id uint64, revokedAt time.Time, reason string) (bool, error) {
	res := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{"revoked_at": revokedAt, "revoked_reason": reason})
	if res.Error != nil {
		return false, apperr.Wrap(apperr.CodeDatabase, "revoke refresh token failed", res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *gormRepository) RevokeOthersForUser(ctx context.Context, userID, keepID uint64, revokedAt time.Time, reason string) error {
	err := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL AND id <> ?", userID, keepID).
		Updates(map[string]any{"revoked_at": revokedAt, "revoked_reason": reason}).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "revoke sibling refresh tokens failed", err)
	}
	sessionRevocationsTotal.WithLabelValues(reason).Inc()
	return nil
}

func (r *gormRepository) RetireRotatedForUser(ctx context.Context, userID uint64, reason string) error {
	err := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NOT NULL AND revoked_reason = ?", userID, RevokeRotated).
		Update("revoked_reason", reason).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "retire rotated refresh tokens failed", err)
	}
	return nil
}

func (r *gormRepository) RevokeAllForUser(ctx context.Context, userID uint64, revokedAt time.Time, reason string) error {
	err := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Updates(map[string]any{"revoked_at": revokedAt, "revoked_reason": reason}).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "revoke refresh tokens failed", err)
	}
	sessionRevocationsTotal.WithLabelValues(reason).Inc()
	return nil
}

// PurgeExpired deletes rows by expiry, not by revocation: a revoked session is
// kept until it would have expired anyway, so a replay attempt still gets the
// documented "revoked, not unknown" answer inside its lifetime.
func (r *gormRepository) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("expires_at < ?", before).
		Delete(&RefreshToken{})
	if res.Error != nil {
		return 0, apperr.Wrap(apperr.CodeDatabase, "purge expired refresh tokens failed", res.Error)
	}
	return res.RowsAffected, nil
}
