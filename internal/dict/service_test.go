package dict

import (
	"context"
	"errors"
	"testing"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// fakeRepository embeds Repository so tests only override the methods they
// exercise; calling anything else panics, which is the desired loud failure.
type fakeRepository struct {
	Repository

	groups map[string]*Group
	items  map[uint64][]Item

	replacedGroup *Group
	replacedItems []Item
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{groups: make(map[string]*Group), items: make(map[uint64][]Item)}
}

func (r *fakeRepository) FindGroupByKey(_ context.Context, key string) (*Group, error) {
	group, ok := r.groups[key]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	copied := *group
	return &copied, nil
}

func (r *fakeRepository) FindGroupByID(_ context.Context, id uint64) (*Group, error) {
	for _, group := range r.groups {
		if group.ID == id {
			copied := *group
			return &copied, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (r *fakeRepository) CreateGroup(_ context.Context, group *Group) error {
	if group.ID == 0 {
		group.ID = uint64(len(r.groups) + 1)
	}
	copied := *group
	r.groups[group.Key] = &copied
	return nil
}

func (r *fakeRepository) ReplaceGroupItems(_ context.Context, group *Group, items []Item) (*Group, error) {
	if existing, ok := r.groups[group.Key]; ok {
		group.ID = existing.ID
	} else {
		group.ID = uint64(len(r.groups) + 1)
		copied := *group
		r.groups[group.Key] = &copied
	}
	copiedItems := append([]Item(nil), items...)
	r.replacedGroup = group
	r.replacedItems = copiedItems
	r.items[group.ID] = copiedItems
	return group, nil
}

func (r *fakeRepository) ListItems(_ context.Context, groupID uint64, includeDisabled bool) ([]Item, error) {
	var out []Item
	for i := range r.items[groupID] {
		item := &r.items[groupID][i]
		if includeDisabled || item.Enabled() {
			out = append(out, *item)
		}
	}
	return out, nil
}

func (r *fakeRepository) CountItems(_ context.Context, groupIDs []uint64) (map[uint64]int64, error) {
	counts := make(map[uint64]int64, len(groupIDs))
	for _, id := range groupIDs {
		counts[id] = int64(len(r.items[id]))
	}
	return counts, nil
}

func (r *fakeRepository) FindItemByID(_ context.Context, id uint64) (*Item, error) {
	for _, items := range r.items {
		for i := range items {
			if items[i].ID == id {
				copied := items[i]
				return &copied, nil
			}
		}
	}
	return nil, apperr.ErrNotFound
}

func (r *fakeRepository) UpdateItem(_ context.Context, item *Item) error {
	for groupID, items := range r.items {
		for i := range items {
			if items[i].ID == item.ID {
				r.items[groupID][i] = *item
				return nil
			}
		}
	}
	return apperr.ErrNotFound
}

func TestCreateGroupRejectsInvalidAndDuplicateKeys(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	if _, err := svc.CreateGroup(context.Background(), CreateGroupInput{Key: "BAD KEY", Name: "x", Status: StatusEnabled}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("invalid key error = %v, want invalid argument", err)
	}

	group, err := svc.CreateGroup(context.Background(), CreateGroupInput{Key: "user.status", Name: "User status", Status: StatusEnabled})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if group.Name != "User status" || group.Status != StatusEnabled {
		t.Errorf("created group = %+v", group)
	}

	if _, err := svc.CreateGroup(context.Background(), CreateGroupInput{Key: "user.status", Name: "dup", Status: StatusEnabled}); !isCode(err, apperr.CodeConflict) {
		t.Errorf("duplicate key error = %v, want conflict", err)
	}
}

func TestGetFiltersDisabledGroupAndItems(t *testing.T) {
	repo := newFakeRepository()
	repo.groups["user.status"] = &Group{ID: 1, Key: "user.status", Status: StatusDisabled}
	repo.items[1] = []Item{
		{ID: 1, GroupID: 1, Key: "1", Label: "Active", Status: StatusEnabled},
		{ID: 2, GroupID: 1, Key: "0", Label: "Disabled", Status: StatusDisabled},
	}
	svc := NewService(repo)

	if _, err := svc.Get(context.Background(), "user.status"); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("disabled group error = %v, want not found", err)
	}

	repo.groups["user.status"].Status = StatusEnabled
	items, err := svc.Get(context.Background(), "user.status")
	if err != nil {
		t.Fatalf("get enabled group: %v", err)
	}
	if len(items) != 1 || items[0].Key != "1" {
		t.Errorf("items = %+v, want only the enabled item", items)
	}
}

func TestSetReplacesObjectAsSortedItems(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	err := svc.Set(context.Background(), "site.seo", map[string]any{
		"title":  "Example",
		"robots": "index",
		"count":  3,
		"nested": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if repo.replacedItems == nil {
		t.Fatal("Set must replace the group items through the repository")
	}

	wantKeys := []string{"count", "nested", "robots", "title"}
	wantTypes := []ValueType{ValueTypeNumber, ValueTypeJSON, ValueTypeString, ValueTypeString}
	if len(repo.replacedItems) != len(wantKeys) {
		t.Fatalf("items = %d, want %d", len(repo.replacedItems), len(wantKeys))
	}
	for i := range wantKeys {
		item := repo.replacedItems[i]
		if item.Key != wantKeys[i] || item.ValueType != wantTypes[i] || item.SortOrder != i {
			t.Errorf("item[%d] = %+v, want key=%s type=%s sort=%d", i, item, wantKeys[i], wantTypes[i], i)
		}
	}
	if repo.replacedGroup == nil || repo.replacedGroup.ID == 0 {
		t.Error("Set must create the group when it does not exist")
	}
}

func TestUpdateMapsArrayToItems(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	err := svc.Update(context.Background(), "keywords", []any{
		"go",
		"gin",
		map[string]any{"key": "gorm", "label": "GORM", "value": 2, "sort": 5},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(repo.replacedItems) != 3 {
		t.Fatalf("items = %d, want 3", len(repo.replacedItems))
	}
	if repo.replacedItems[0].Key != "go" || repo.replacedItems[0].SortOrder != 0 {
		t.Errorf("primitive item = %+v", repo.replacedItems[0])
	}
	structured := repo.replacedItems[2]
	if structured.Key != "gorm" || structured.Label != "GORM" || structured.Value != "2" || structured.ValueType != ValueTypeNumber || structured.SortOrder != 5 {
		t.Errorf("structured item = %+v", structured)
	}
}

func TestGetIntoDecodesByValueType(t *testing.T) {
	repo := newFakeRepository()
	repo.groups["site.seo"] = &Group{ID: 1, Key: "site.seo", Status: StatusEnabled}
	repo.items[1] = []Item{
		{Key: "title", Value: "Example", ValueType: ValueTypeString, Status: StatusEnabled},
		{Key: "count", Value: "3", ValueType: ValueTypeNumber, Status: StatusEnabled},
		{Key: "enabled", Value: "true", ValueType: ValueTypeBoolean, Status: StatusEnabled},
		{Key: "links", Value: `["a","b"]`, ValueType: ValueTypeJSON, Status: StatusEnabled},
		{Key: "fallback", ValueType: ValueTypeString, Status: StatusEnabled},
	}
	svc := NewService(repo)

	var out struct {
		Title    string   `json:"title"`
		Count    int      `json:"count"`
		Enabled  bool     `json:"enabled"`
		Links    []string `json:"links"`
		Fallback string   `json:"fallback"`
	}
	if err := svc.GetInto(context.Background(), "site.seo", &out); err != nil {
		t.Fatalf("get into: %v", err)
	}
	if out.Title != "Example" || out.Count != 3 || !out.Enabled || len(out.Links) != 2 || out.Fallback != "fallback" {
		t.Errorf("decoded = %+v", out)
	}

	var asMap map[string]any
	if err := svc.GetInto(context.Background(), "site.seo", &asMap); err != nil {
		t.Fatalf("get into map: %v", err)
	}
	if asMap["title"] != "Example" {
		t.Errorf("map title = %v", asMap["title"])
	}

	var asSlice []any
	if err := svc.GetInto(context.Background(), "site.seo", &asSlice); err != nil {
		t.Fatalf("get into slice: %v", err)
	}
	if len(asSlice) != 5 {
		t.Errorf("slice len = %d, want 5", len(asSlice))
	}
}

func isCode(err error, code apperr.Code) bool {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}
