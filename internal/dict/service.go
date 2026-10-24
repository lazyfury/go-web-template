package dict

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/go-web-template/internal/platform/apperr"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxLabelRunes   = 128
	maxValueRunes   = 1024
	maxRemarkRunes  = 255
)

// groupKeyPattern keeps group keys URL-safe and stable: consumers reference
// them from code and the API path.
var groupKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// Store is the narrow convenience API other domains depend on. It exposes
// whole-setting reads/writes instead of the normalized group/item CRUD.
type Store interface {
	// Get returns the enabled dict array of a group, ordered by sort_order.
	Get(ctx context.Context, group string) ([]Item, error)
	// GetInto loads a group into dst: a map, a slice, or a struct with
	// matching json tags.
	GetInto(ctx context.Context, group string, dst any) error
	// Replace replaces a whole setting with an object or an array value.
	// Objects become key/value items (sorted by key); arrays become ordered
	// items (sort_order = index). Any other value is rejected.
	Replace(ctx context.Context, group string, value any) error
}

// Service implements dictionary use-cases. It depends on Repository only, so
// the rules are unit-testable without a database.
type Service struct {
	repo Repository
}

var _ Store = (*Service)(nil)

// NewService creates a Service backed by the given repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateGroupInput is the validated input for creating a group.
type CreateGroupInput struct {
	Key         string
	Name        string
	Description string
	Status      Status
}

// CreateGroupPatch is a partial update; nil means "not provided".
type CreateGroupPatch struct {
	Name        *string
	Description *string
	Status      *Status
}

// CreateItemInput is the validated input for creating an item.
type CreateItemInput struct {
	Key       string
	Label     string
	Value     string
	ValueType ValueType
	SortOrder int
	Status    Status
	Remark    string
}

// CreateItemPatch is a partial item update; nil means "not provided".
type CreateItemPatch struct {
	Key       *string
	Label     *string
	Value     *string
	ValueType *ValueType
	SortOrder *int
	Status    *Status
	Remark    *string
}

// CreateGroup validates and persists a new group. Keys are immutable and
// unique among non-deleted groups.
func (s *Service) CreateGroup(ctx context.Context, in CreateGroupInput) (*Group, error) {
	key, err := normalizeGroupKey(in.Key)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = key
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, invalid("group name must be at most 64 characters")
	}
	description := strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(description) > 255 {
		return nil, invalid("group description must be at most 255 characters")
	}
	status := in.Status
	if !status.Valid() {
		return nil, invalid("invalid group status")
	}

	if _, err := s.repo.FindGroupByKey(ctx, key); err == nil {
		return nil, apperr.Wrap(apperr.CodeConflict, "dict group already exists", nil)
	} else if !errors.Is(err, apperr.ErrNotFound) {
		return nil, err
	}

	group := &Group{
		Key:         key,
		Name:        name,
		Description: description,
		Status:      status,
	}
	if err := s.repo.CreateGroup(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

// GetGroup returns a group and the number of items it owns.
func (s *Service) GetGroup(ctx context.Context, id uint64) (*Group, int64, error) {
	group, err := s.repo.FindGroupByID(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	counts, err := s.repo.CountItems(ctx, []uint64{id})
	if err != nil {
		return nil, 0, err
	}
	return group, counts[id], nil
}

// ListGroups returns a page of groups plus per-group item counts.
func (s *Service) ListGroups(ctx context.Context, page, pageSize int, keyword string) (groups []Group, counts map[uint64]int64, total int64, err error) {
	page, pageSize = normalizePagination(page, pageSize)
	groups, total, err = s.repo.ListGroups(ctx, (page-1)*pageSize, pageSize, strings.TrimSpace(keyword))
	if err != nil {
		return nil, nil, 0, err
	}
	ids := make([]uint64, 0, len(groups))
	for i := range groups {
		ids = append(ids, groups[i].ID)
	}
	counts, err = s.repo.CountItems(ctx, ids)
	if err != nil {
		return nil, nil, 0, err
	}
	return groups, counts, total, nil
}

// UpdateGroup applies a partial update. The key is intentionally immutable.
func (s *Service) UpdateGroup(ctx context.Context, id uint64, patch CreateGroupPatch) (*Group, error) {
	group, err := s.repo.FindGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" || utf8.RuneCountInString(name) > 64 {
			return nil, invalid("group name must be between 1 and 64 characters")
		}
		group.Name = name
	}
	if patch.Description != nil {
		description := strings.TrimSpace(*patch.Description)
		if utf8.RuneCountInString(description) > 255 {
			return nil, invalid("group description must be at most 255 characters")
		}
		group.Description = description
	}
	if patch.Status != nil {
		if !patch.Status.Valid() {
			return nil, invalid("invalid group status")
		}
		group.Status = *patch.Status
	}

	if err := s.repo.UpdateGroup(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

// DeleteGroup soft-deletes a group and its items.
func (s *Service) DeleteGroup(ctx context.Context, id uint64) error {
	return s.repo.DeleteGroup(ctx, id)
}

// ListItems returns the items of a group. includeDisabled is for the admin UI;
// consumers always use Get, which filters disabled rows.
func (s *Service) ListItems(ctx context.Context, groupID uint64, includeDisabled bool) ([]Item, error) {
	if _, err := s.repo.FindGroupByID(ctx, groupID); err != nil {
		return nil, err
	}
	return s.repo.ListItems(ctx, groupID, includeDisabled)
}

// CreateItem validates and persists an item under an existing group.
func (s *Service) CreateItem(ctx context.Context, groupID uint64, in *CreateItemInput) (*Item, error) {
	if _, err := s.repo.FindGroupByID(ctx, groupID); err != nil {
		return nil, err
	}
	item, err := buildItem(in)
	if err != nil {
		return nil, err
	}
	item.GroupID = groupID
	if err := s.repo.CreateItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateItem applies a partial update to an item.
func (s *Service) UpdateItem(ctx context.Context, id uint64, patch CreateItemPatch) (*Item, error) {
	item, err := s.repo.FindItemByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if patch.Key != nil {
		key, err := normalizeItemKey(*patch.Key)
		if err != nil {
			return nil, err
		}
		item.Key = key
	}
	if patch.Label != nil {
		label := strings.TrimSpace(*patch.Label)
		if label == "" || utf8.RuneCountInString(label) > maxLabelRunes {
			return nil, invalid("item label must be between 1 and 128 characters")
		}
		item.Label = label
	}
	if patch.Value != nil {
		if utf8.RuneCountInString(*patch.Value) > maxValueRunes {
			return nil, invalid("item value must be at most 1024 characters")
		}
		item.Value = *patch.Value
	}
	if patch.ValueType != nil {
		if !patch.ValueType.Valid() {
			return nil, invalid("invalid item value_type")
		}
		item.ValueType = *patch.ValueType
	}
	if patch.SortOrder != nil {
		item.SortOrder = *patch.SortOrder
	}
	if patch.Status != nil {
		if !patch.Status.Valid() {
			return nil, invalid("invalid item status")
		}
		item.Status = *patch.Status
	}
	if patch.Remark != nil {
		remark := strings.TrimSpace(*patch.Remark)
		if utf8.RuneCountInString(remark) > maxRemarkRunes {
			return nil, invalid("item remark must be at most 255 characters")
		}
		item.Remark = remark
	}

	if err := s.repo.UpdateItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// DeleteItem soft-deletes one item.
func (s *Service) DeleteItem(ctx context.Context, id uint64) error {
	return s.repo.DeleteItem(ctx, id)
}

// Get returns the enabled dict array of a group. A missing or disabled group
// is reported as not found so consumers never act on a disabled setting.
func (s *Service) Get(ctx context.Context, group string) ([]Item, error) {
	key, err := normalizeGroupKey(group)
	if err != nil {
		return nil, err
	}
	found, err := s.repo.FindGroupByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found.Enabled() {
		return nil, apperr.ErrNotFound
	}
	return s.repo.ListItems(ctx, found.ID, false)
}

// GetInto loads a group into dst. A map receives key→decoded value, a slice
// receives the values in sort order, and any other pointer is decoded through
// JSON (so structs with json tags work).
func (s *Service) GetInto(ctx context.Context, group string, dst any) error {
	items, err := s.Get(ctx, group)
	if err != nil {
		return err
	}
	if dst == nil {
		return invalid("destination must be a non-nil pointer")
	}

	asMap := make(map[string]any, len(items))
	asSlice := make([]any, 0, len(items))
	for i := range items {
		value := items[i].DecodedValue()
		asMap[items[i].Key] = value
		asSlice = append(asSlice, value)
	}

	kind, err := pointerKind(dst)
	if err != nil {
		return err
	}
	switch kind {
	case destinationSlice:
		return decodeJSON(asSlice, dst)
	case destinationMap:
		return decodeJSON(asMap, dst)
	default:
		return decodeJSON(asMap, dst)
	}
}

// Replace replaces a whole setting. An object value becomes key/value items
// sorted by field name; an array value becomes items ordered by index. The
// group is created when missing; existing admin-managed metadata is preserved.
func (s *Service) Replace(ctx context.Context, group string, value any) error {
	key, err := normalizeGroupKey(group)
	if err != nil {
		return err
	}

	var items []Item
	if fields, ok := toAnyMap(value); ok {
		items, err = itemsFromObject(fields)
	} else if list, ok := toAnySlice(value); ok {
		items, err = itemsFromList(list)
	} else {
		return invalid("dict setting value must be an object or an array")
	}
	if err != nil {
		return err
	}

	_, err = s.repo.ReplaceGroupItems(ctx, &Group{
		Key:    key,
		Name:   key,
		Status: StatusEnabled,
	}, items)
	return err
}

func buildItem(in *CreateItemInput) (*Item, error) {
	key, err := normalizeItemKey(in.Key)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = key
	}
	if utf8.RuneCountInString(label) > maxLabelRunes {
		return nil, invalid("item label must be at most 128 characters")
	}
	if utf8.RuneCountInString(in.Value) > maxValueRunes {
		return nil, invalid("item value must be at most 1024 characters")
	}
	valueType := in.ValueType
	if valueType == "" {
		valueType = ValueTypeString
	}
	if !valueType.Valid() {
		return nil, invalid("invalid item value_type")
	}
	status := in.Status
	if !status.Valid() {
		return nil, invalid("invalid item status")
	}
	remark := strings.TrimSpace(in.Remark)
	if utf8.RuneCountInString(remark) > maxRemarkRunes {
		return nil, invalid("item remark must be at most 255 characters")
	}
	return &Item{
		Key:       key,
		Label:     label,
		Value:     in.Value,
		ValueType: valueType,
		SortOrder: in.SortOrder,
		Status:    status,
		Remark:    remark,
	}, nil
}

func normalizeGroupKey(raw string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if !groupKeyPattern.MatchString(key) {
		return "", invalid("invalid group key: use lowercase letters, digits, '_', '.' or '-' (max 64)")
	}
	return key, nil
}

func normalizeItemKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || utf8.RuneCountInString(key) > 64 {
		return "", invalid("item key must be between 1 and 64 characters")
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return "", invalid("item key must not contain control characters")
		}
	}
	return key, nil
}

func normalizePagination(page, size int) (normalizedPage, normalizedSize int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > maxPageSize {
		size = defaultPageSize
	}
	return page, size
}

func invalid(message string) error {
	return apperr.Wrap(apperr.CodeInvalidArgument, message, nil)
}
