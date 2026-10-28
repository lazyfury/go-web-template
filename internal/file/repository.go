package file

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// ListFilter narrows a file listing. OwnerID nil means "every owner" and is
// only used by the admin path; the service forces the caller's id for members.
type ListFilter struct {
	OwnerID  *uint64
	BizType  string
	Keyword  string
	Page     int
	PageSize int
}

// Repository is the persistence contract for the file module.
type Repository interface {
	Create(ctx context.Context, f *File) error
	FindByID(ctx context.Context, id uint64) (*File, error)
	List(ctx context.Context, filter ListFilter) ([]File, int64, error)
	UpdateVisibility(ctx context.Context, id uint64, visibility Visibility) error
	// MarkDeleting soft-deletes the row and moves it to status=deleting. The
	// blob is removed later by the housekeeping worker.
	MarkDeleting(ctx context.Context, id uint64, at time.Time) error
	// HardDelete removes the row after its blob has been deleted.
	HardDelete(ctx context.Context, id uint64) error
	// ListByStatusBefore returns rows in status older than before (purge input).
	ListByStatusBefore(ctx context.Context, status Status, before time.Time, limit int) ([]File, error)
	// ListOrphans returns ready rows older than before that no active link
	// references (abandoned uploads).
	ListOrphans(ctx context.Context, before time.Time, limit int) ([]File, error)

	// Shares (long-lived public asset links).
	CreateShare(ctx context.Context, share *Share) error
	FindActiveShareByToken(ctx context.Context, token string, now time.Time) (*Share, error)
	FindActiveShareByPurpose(ctx context.Context, fileID uint64, purpose string, now time.Time) (*Share, error)
	RevokeShares(ctx context.Context, fileID uint64, purpose string, at time.Time) error
	// UsageSince reports one owner's upload count and byte total since a cutoff
	// (rolling quota enforcement).
	UsageSince(ctx context.Context, ownerID uint64, since time.Time) (files int64, bytes int64, err error)

	CreateLink(ctx context.Context, link *Link) error
	SoftDeleteLinks(ctx context.Context, fileID uint64, ownerType string, ownerID uint64, slot string, at time.Time) error
	SoftDeleteLinksByOwner(ctx context.Context, ownerType string, ownerID uint64, at time.Time) error
	ListFilesByOwner(ctx context.Context, ownerType string, ownerID uint64) ([]File, error)
	CountActiveLinks(ctx context.Context, fileID uint64) (int64, error)
}

type gormRepository struct {
	db *gorm.DB
}

// NewRepository creates a GORM-backed Repository.
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(ctx context.Context, f *File) error {
	if err := r.db.WithContext(ctx).Create(f).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "file already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "create file failed", err)
	}
	return nil
}

// FindByID never returns soft-deleted rows: deleted_at is managed explicitly
// (plain *time.Time does not enable GORM's soft-delete scope).
func (r *gormRepository) FindByID(ctx context.Context, id uint64) (*File, error) {
	var f File
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find file failed", err)
	}
	return &f, nil
}

func (r *gormRepository) List(ctx context.Context, filter ListFilter) ([]File, int64, error) {
	query := r.db.WithContext(ctx).Model(&File{}).Where("deleted_at IS NULL")
	if filter.OwnerID != nil {
		query = query.Where("owner_id = ?", *filter.OwnerID)
	}
	if filter.BizType != "" {
		query = query.Where("biz_type = ?", filter.BizType)
	}
	if filter.Keyword != "" {
		// LOWER() keeps the filter portable across PostgreSQL and SQLite.
		like := "%" + strings.ToLower(filter.Keyword) + "%"
		query = query.Where("LOWER(original_name) LIKE ?", like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count files failed", err)
	}
	var files []File
	if err := query.Order("id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&files).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list files failed", err)
	}
	return files, total, nil
}

func (r *gormRepository) UpdateVisibility(ctx context.Context, id uint64, visibility Visibility) error {
	res := r.db.WithContext(ctx).Model(&File{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"visibility": visibility, "updated_at": time.Now()})
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "update file visibility failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *gormRepository) MarkDeleting(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&File{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"status": StatusDeleting, "deleted_at": at, "updated_at": at})
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "mark file deleting failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *gormRepository) HardDelete(ctx context.Context, id uint64) error {
	if err := r.db.WithContext(ctx).Delete(&File{}, id).Error; err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "delete file failed", err)
	}
	return nil
}

func (r *gormRepository) ListByStatusBefore(ctx context.Context, status Status, before time.Time, limit int) ([]File, error) {
	var files []File
	err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", status, before).
		Order("id").
		Limit(limit).
		Find(&files).Error
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "list files by status failed", err)
	}
	return files, nil
}

func (r *gormRepository) ListOrphans(ctx context.Context, before time.Time, limit int) ([]File, error) {
	var files []File
	err := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL AND created_at < ?", StatusReady, before).
		Where("NOT EXISTS (SELECT 1 FROM file_links WHERE file_links.file_id = files.id AND file_links.deleted_at IS NULL)").
		Order("id").
		Limit(limit).
		Find(&files).Error
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "list orphan files failed", err)
	}
	return files, nil
}

func (r *gormRepository) CreateShare(ctx context.Context, share *Share) error {
	if err := r.db.WithContext(ctx).Create(share).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "share already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "create file share failed", err)
	}
	return nil
}

func (r *gormRepository) FindActiveShareByToken(ctx context.Context, token string, now time.Time) (*Share, error) {
	var share Share
	err := r.db.WithContext(ctx).
		Where("token = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", token, now).
		First(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find file share failed", err)
	}
	return &share, nil
}

func (r *gormRepository) FindActiveShareByPurpose(ctx context.Context, fileID uint64, purpose string, now time.Time) (*Share, error) {
	var share Share
	err := r.db.WithContext(ctx).
		Where("file_id = ? AND purpose = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", fileID, purpose, now).
		First(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find file share by purpose failed", err)
	}
	return &share, nil
}

func (r *gormRepository) RevokeShares(ctx context.Context, fileID uint64, purpose string, at time.Time) error {
	query := r.db.WithContext(ctx).Model(&Share{}).
		Where("file_id = ? AND revoked_at IS NULL", fileID)
	if purpose != "" {
		query = query.Where("purpose = ?", purpose)
	}
	if err := query.Update("revoked_at", at).Error; err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "revoke file shares failed", err)
	}
	return nil
}

func (r *gormRepository) UsageSince(ctx context.Context, ownerID uint64, since time.Time) (fileCount, byteCount int64, err error) {
	var usage struct {
		Files int64
		Bytes int64
	}
	err = r.db.WithContext(ctx).
		Model(&File{}).
		Select("COUNT(*) AS files, COALESCE(SUM(size_bytes), 0) AS bytes").
		Where("owner_id = ? AND deleted_at IS NULL AND created_at >= ?", ownerID, since).
		Scan(&usage).Error
	if err != nil {
		return 0, 0, apperr.Wrap(apperr.CodeDatabase, "count upload usage failed", err)
	}
	return usage.Files, usage.Bytes, nil
}

func (r *gormRepository) CreateLink(ctx context.Context, link *Link) error {
	// Idempotent attach: the unique partial index turns a duplicate into a
	// no-op instead of a 409 the caller would have to special-case.
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(link).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "create file link failed", err)
	}
	return nil
}

func (r *gormRepository) SoftDeleteLinks(ctx context.Context, fileID uint64, ownerType string, ownerID uint64, slot string, at time.Time) error {
	query := r.db.WithContext(ctx).Model(&Link{}).
		Where("file_id = ? AND owner_type = ? AND owner_id = ? AND deleted_at IS NULL", fileID, ownerType, ownerID)
	if slot != "" {
		query = query.Where("slot = ?", slot)
	}
	if err := query.Update("deleted_at", at).Error; err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "detach file failed", err)
	}
	return nil
}

func (r *gormRepository) SoftDeleteLinksByOwner(ctx context.Context, ownerType string, ownerID uint64, at time.Time) error {
	err := r.db.WithContext(ctx).Model(&Link{}).
		Where("owner_type = ? AND owner_id = ? AND deleted_at IS NULL", ownerType, ownerID).
		Update("deleted_at", at).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "detach owner files failed", err)
	}
	return nil
}

func (r *gormRepository) ListFilesByOwner(ctx context.Context, ownerType string, ownerID uint64) ([]File, error) {
	var files []File
	err := r.db.WithContext(ctx).
		Model(&File{}).
		Joins("JOIN file_links ON file_links.file_id = files.id").
		Where("file_links.owner_type = ? AND file_links.owner_id = ? AND file_links.deleted_at IS NULL", ownerType, ownerID).
		Where("files.deleted_at IS NULL").
		Order("file_links.id").
		Find(&files).Error
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "list owner files failed", err)
	}
	return files, nil
}

func (r *gormRepository) CountActiveLinks(ctx context.Context, fileID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&Link{}).
		Where("file_id = ? AND deleted_at IS NULL", fileID).
		Count(&count).Error
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeDatabase, "count file links failed", err)
	}
	return count, nil
}

// isUniqueViolation detects PostgreSQL unique-constraint violations across pgx
// error shapes (same helper shape as the other domains).
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique violation")
}
