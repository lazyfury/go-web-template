package user

import (
	"context"
	"errors"
	"testing"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/testutil"
)

func TestRepositoryCRUD(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &User{})
	repo := NewRepository(db)
	ctx := context.Background()

	// Create.
	user := &User{Name: "Repo Tester", Email: "repo@example.com", Phone: "+10000000000", Status: StatusActive}
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
	if got.Phone != "+10000000000" {
		t.Errorf("phone = %q, want +10000000000", got.Phone)
	}
	if _, err := repo.FindByEmail(ctx, "REPO@example.com"); err != nil {
		t.Errorf("FindByEmail should be case-insensitive on lookup, got %v", err)
	}

	// Not found.
	if _, err := repo.FindByID(ctx, 99999); !errors.Is(err, apperr.ErrNotFound) {
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

	// Update, including clearing the phone (zero value must persist).
	user.Name = "Renamed"
	user.Phone = ""
	if err := repo.Update(ctx, user); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.FindByID(ctx, user.ID)
	if got.Name != "Renamed" {
		t.Errorf("name after update = %q", got.Name)
	}
	if got.Phone != "" {
		t.Errorf("phone after clear = %q, want empty", got.Phone)
	}

	// Soft delete: row disappears from normal queries.
	if err := repo.Delete(ctx, user.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.FindByID(ctx, user.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected not found after delete, got %v", err)
	}
	if err := repo.Delete(ctx, user.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("second delete expected not found, got %v", err)
	}
}

func TestRepositorySaveAuthStatePersistsZeroValues(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &User{})
	repo := NewRepository(db)
	ctx := context.Background()

	u := &User{Name: "Auth", Email: "auth@example.com", Status: StatusActive, PasswordHash: "hash-1"}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate a failed login followed by a successful one: the counter must go
	// back to 0 (Updates(struct) alone would skip the zero value).
	u.FailedLoginCount = 4
	u.Name = "Auth Renamed"
	u.Phone = "+20000000000"
	if err := repo.SaveAuthState(ctx, u); err != nil {
		t.Fatalf("SaveAuthState: %v", err)
	}
	u.FailedLoginCount = 0
	u.TokenVersion = 3
	u.PasswordHash = "hash-2"
	if err := repo.SaveAuthState(ctx, u); err != nil {
		t.Fatalf("SaveAuthState reset: %v", err)
	}

	got, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.FailedLoginCount != 0 {
		t.Errorf("failed login count = %d, want 0", got.FailedLoginCount)
	}
	if got.TokenVersion != 3 {
		t.Errorf("token version = %d, want 3", got.TokenVersion)
	}
	if got.PasswordHash != "hash-2" {
		t.Errorf("password hash = %q, want hash-2", got.PasswordHash)
	}
	if got.Name != "Auth Renamed" || got.Phone != "+20000000000" {
		t.Errorf("profile columns not persisted by SaveAuthState: name=%q phone=%q", got.Name, got.Phone)
	}
}
