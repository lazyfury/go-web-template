package dict

import (
	"context"
	"errors"
	"testing"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/testutil"
)

func TestRepositoryGroupAndItemLifecycle(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &Group{}, &Item{})
	repo := NewRepository(db)
	ctx := context.Background()

	group := &Group{Key: "user.status", Name: "User status", Status: StatusEnabled}
	if err := repo.CreateGroup(ctx, group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if group.ID == 0 {
		t.Fatal("create group must assign an id")
	}
	if _, err := repo.FindGroupByKey(ctx, "user.status"); err != nil {
		t.Fatalf("find group by key: %v", err)
	}
	if err := repo.CreateGroup(ctx, &Group{Key: "user.status", Name: "dup", Status: StatusEnabled}); !isCode(err, apperr.CodeConflict) {
		t.Errorf("duplicate group error = %v, want conflict", err)
	}

	active := &Item{GroupID: group.ID, Key: "1", Label: "Active", ValueType: ValueTypeString, Status: StatusEnabled, SortOrder: 20}
	disabled := &Item{GroupID: group.ID, Key: "0", Label: "Disabled", ValueType: ValueTypeString, Status: StatusDisabled, SortOrder: 10}
	if err := repo.CreateItem(ctx, active); err != nil {
		t.Fatalf("create active item: %v", err)
	}
	if err := repo.CreateItem(ctx, disabled); err != nil {
		t.Fatalf("create disabled item: %v", err)
	}
	if err := repo.CreateItem(ctx, &Item{GroupID: group.ID, Key: "1", Label: "Dup", ValueType: ValueTypeString, Status: StatusEnabled}); !isCode(err, apperr.CodeConflict) {
		t.Errorf("duplicate item error = %v, want conflict", err)
	}

	enabled, err := repo.ListItems(ctx, group.ID, false)
	if err != nil {
		t.Fatalf("list enabled items: %v", err)
	}
	if len(enabled) != 1 || enabled[0].Key != "1" {
		t.Errorf("enabled items = %+v, want only key 1", enabled)
	}
	all, err := repo.ListItems(ctx, group.ID, true)
	if err != nil {
		t.Fatalf("list all items: %v", err)
	}
	if len(all) != 2 || all[0].Key != "0" || all[1].Key != "1" {
		t.Errorf("all items = %+v, want sort 10 then 20", all)
	}

	counts, err := repo.CountItems(ctx, []uint64{group.ID})
	if err != nil {
		t.Fatalf("count items: %v", err)
	}
	if counts[group.ID] != 2 {
		t.Errorf("item count = %d, want 2", counts[group.ID])
	}
}

func TestRepositoryReplaceGroupItems(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &Group{}, &Item{})
	repo := NewRepository(db)
	ctx := context.Background()

	group, err := repo.ReplaceGroupItems(ctx, &Group{Key: "keywords", Name: "keywords", Status: StatusEnabled}, []Item{
		{Key: "go", Label: "go", Value: "go", ValueType: ValueTypeString, Status: StatusEnabled, SortOrder: 0},
		{Key: "gin", Label: "gin", Value: "gin", ValueType: ValueTypeString, Status: StatusEnabled, SortOrder: 1},
	})
	if err != nil {
		t.Fatalf("initial replace: %v", err)
	}

	// Replacing with the same key must not trip the unique index: old rows are
	// purged before the new ones are inserted.
	if _, err := repo.ReplaceGroupItems(ctx, &Group{Key: "keywords", Name: "ignored", Status: StatusEnabled}, []Item{
		{Key: "gorm", Label: "gorm", Value: "gorm", ValueType: ValueTypeString, Status: StatusEnabled, SortOrder: 0},
	}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	items, err := repo.ListItems(ctx, group.ID, true)
	if err != nil {
		t.Fatalf("list after replace: %v", err)
	}
	if len(items) != 1 || items[0].Key != "gorm" {
		t.Errorf("items after replace = %+v, want only gorm", items)
	}

	// The convenience API must not overwrite admin-managed metadata.
	stored, err := repo.FindGroupByKey(ctx, "keywords")
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	if stored.Name != "keywords" {
		t.Errorf("group name = %q, want the existing name", stored.Name)
	}
}

func TestRepositorySoftDeleteAndKeyReuse(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &Group{}, &Item{})
	repo := NewRepository(db)
	ctx := context.Background()

	group := &Group{Key: "site.theme", Name: "Theme", Status: StatusEnabled}
	if err := repo.CreateGroup(ctx, group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	item := &Item{GroupID: group.ID, Key: "dark", Label: "Dark", ValueType: ValueTypeString, Status: StatusEnabled}
	if err := repo.CreateItem(ctx, item); err != nil {
		t.Fatalf("create item: %v", err)
	}

	if err := repo.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if _, err := repo.FindGroupByID(ctx, group.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("find deleted group = %v, want not found", err)
	}
	items, err := repo.ListItems(ctx, group.ID, true)
	if err != nil {
		t.Fatalf("list items of deleted group: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items after group delete = %+v, want empty", items)
	}

	// Partial unique indexes allow reusing the key after a soft delete.
	recreated := &Group{Key: "site.theme", Name: "Theme v2", Status: StatusEnabled}
	if err := repo.CreateGroup(ctx, recreated); err != nil {
		t.Fatalf("recreate group with the same key: %v", err)
	}
}
