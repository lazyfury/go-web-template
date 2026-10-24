package dict

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// Repository is the persistence contract for the dictionary module. The
// service depends on this interface so business rules can be unit-tested with
// fakes while production uses GORM.
type Repository interface {
	CreateGroup(ctx context.Context, group *Group) error
	FindGroupByID(ctx context.Context, id uint64) (*Group, error)
	FindGroupByKey(ctx context.Context, key string) (*Group, error)
	ListGroups(ctx context.Context, offset, limit int, keyword string) ([]Group, int64, error)
	UpdateGroup(ctx context.Context, group *Group) error
	DeleteGroup(ctx context.Context, id uint64) error

	ListItems(ctx context.Context, groupID uint64, includeDisabled bool) ([]Item, error)
	FindItemByID(ctx context.Context, id uint64) (*Item, error)
	CreateItem(ctx context.Context, item *Item) error
	UpdateItem(ctx context.Context, item *Item) error
	DeleteItem(ctx context.Context, id uint64) error

	CountItems(ctx context.Context, groupIDs []uint64) (map[uint64]int64, error)

	// ReplaceGroupItems upserts a group by key and atomically replaces every
	// item of that group. It backs the Set/Update convenience API, where the
	// caller provides a whole object/array rather than individual rows.
	ReplaceGroupItems(ctx context.Context, group *Group, items []Item) (*Group, error)
}

type gormRepository struct {
	db *gorm.DB
}

// NewRepository creates a GORM-backed Repository.
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) CreateGroup(ctx context.Context, group *Group) error {
	if err := r.db.WithContext(ctx).Create(group).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "dict group already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "create dict group failed", err)
	}
	return nil
}

func (r *gormRepository) FindGroupByID(ctx context.Context, id uint64) (*Group, error) {
	var group Group
	err := r.db.WithContext(ctx).First(&group, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find dict group failed", err)
	}
	return &group, nil
}

func (r *gormRepository) FindGroupByKey(ctx context.Context, key string) (*Group, error) {
	var group Group
	err := r.db.WithContext(ctx).Where("group_key = ?", key).First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find dict group by key failed", err)
	}
	return &group, nil
}

func (r *gormRepository) ListGroups(ctx context.Context, offset, limit int, keyword string) ([]Group, int64, error) {
	query := r.db.WithContext(ctx).Model(&Group{})
	if keyword != "" {
		// LOWER() keeps the filter portable across PostgreSQL and SQLite.
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(group_key) LIKE ? OR LOWER(name) LIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count dict groups failed", err)
	}

	var groups []Group
	if err := query.
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&groups).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list dict groups failed", err)
	}
	return groups, total, nil
}

func (r *gormRepository) UpdateGroup(ctx context.Context, group *Group) error {
	// Select keeps zero values (status=0, description="") from being skipped.
	err := r.db.WithContext(ctx).
		Model(group).
		Select("name", "description", "status").
		Updates(group).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "update dict group failed", err)
	}
	return nil
}

func (r *gormRepository) DeleteGroup(ctx context.Context, id uint64) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&Group{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.ErrNotFound
		}
		// Soft-delete the children in the same transaction: a deleted group
		// must never expose items through an old id.
		if err := tx.Where("group_id = ?", id).Delete(&Item{}).Error; err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, apperr.ErrNotFound) {
		return err
	}
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "delete dict group failed", err)
	}
	return nil
}

func (r *gormRepository) ListItems(ctx context.Context, groupID uint64, includeDisabled bool) ([]Item, error) {
	query := r.db.WithContext(ctx).Where("group_id = ?", groupID)
	if !includeDisabled {
		query = query.Where("status = ?", StatusEnabled)
	}

	var items []Item
	if err := query.
		Order("sort_order ASC, id ASC").
		Find(&items).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "list dict items failed", err)
	}
	return items, nil
}

func (r *gormRepository) FindItemByID(ctx context.Context, id uint64) (*Item, error) {
	var item Item
	err := r.db.WithContext(ctx).First(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find dict item failed", err)
	}
	return &item, nil
}

func (r *gormRepository) CreateItem(ctx context.Context, item *Item) error {
	if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "dict item already exists in this group", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "create dict item failed", err)
	}
	return nil
}

func (r *gormRepository) UpdateItem(ctx context.Context, item *Item) error {
	err := r.db.WithContext(ctx).
		Model(item).
		Select("item_key", "label", "value", "value_type", "sort_order", "status", "remark").
		Updates(item).Error
	if err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "dict item already exists in this group", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "update dict item failed", err)
	}
	return nil
}

func (r *gormRepository) DeleteItem(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Delete(&Item{}, id)
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "delete dict item failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *gormRepository) CountItems(ctx context.Context, groupIDs []uint64) (map[uint64]int64, error) {
	out := make(map[uint64]int64, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}

	var rows []struct {
		GroupID uint64
		Count   int64
	}
	err := r.db.WithContext(ctx).
		Model(&Item{}).
		Select("group_id, COUNT(*) AS count").
		Where("group_id IN ?", groupIDs).
		Group("group_id").
		Scan(&rows).Error
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "count dict items failed", err)
	}
	for _, row := range rows {
		out[row.GroupID] = row.Count
	}
	return out, nil
}

func (r *gormRepository) ReplaceGroupItems(ctx context.Context, group *Group, items []Item) (*Group, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Group
		err := tx.Where("group_key = ?", group.Key).First(&existing).Error
		switch {
		case err == nil:
			// Keep admin-managed metadata; the convenience API only owns items.
			group.ID = existing.ID
			group.Name = existing.Name
			group.Description = existing.Description
			group.Status = existing.Status
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(group).Error; err != nil {
				return err
			}
		default:
			return err
		}

		// Whole-value replacement: old rows are purged instead of accumulated,
		// because settings can be written frequently and history lives in logs.
		if err := tx.Unscoped().Where("group_id = ?", group.ID).Delete(&Item{}).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].ID = 0
			items[i].GroupID = group.ID
		}
		if len(items) > 0 {
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperr.Wrap(apperr.CodeConflict, "dict group already exists", err)
		}
		return nil, apperr.Wrap(apperr.CodeDatabase, "replace dict group items failed", err)
	}
	return group, nil
}

// isUniqueViolation detects unique-constraint violations across PostgreSQL
// (SQLSTATE 23505) and SQLite ("UNIQUE constraint failed") error shapes.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(fmt.Sprintf("%v", err))
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
}
