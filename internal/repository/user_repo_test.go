package repository

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/model"
	"github.com/example/go-web-template/internal/testutil"
)

func TestUserRepositoryCRUD(t *testing.T) {
	db := testutil.NewSQLiteDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Create.
	user := &model.User{Name: "Repo Tester", Email: "repo@example.com", Status: model.UserStatusActive}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("expected generated id")
	}

	// Find by id / email.
	got, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Email != "repo@example.com" {
		t.Errorf("email = %q", got.Email)
	}
	if _, err := repo.FindByEmail(ctx, "REPO@example.com"); err != nil {
		t.Errorf("FindByEmail should be case-insensitive on lookup, got %v", err)
	}

	// Not found.
	if _, err := repo.FindByID(ctx, 99999); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}

	// List + count.
	users, total, err := repo.List(ctx, 0, 10, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(users) != 1 {
		t.Errorf("list total=%d len=%d, want 1/1", total, len(users))
	}
	_, total, err = repo.List(ctx, 0, 10, "nope")
	if err != nil || total != 0 {
		t.Errorf("keyword list total=%d err=%v, want 0", total, err)
	}

	// Update.
	user.Name = "Renamed"
	if err := repo.Update(ctx, user); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.FindByID(ctx, user.ID)
	if got.Name != "Renamed" {
		t.Errorf("name after update = %q", got.Name)
	}

	// Soft delete: row disappears from normal queries.
	if err := repo.Delete(ctx, user.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.FindByID(ctx, user.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("expected not found after delete, got %v", err)
	}
	if err := repo.Delete(ctx, user.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("second delete expected not found, got %v", err)
	}
}
