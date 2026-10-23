package user

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// mockUserRepo is a hand-written fake implementing Repository.
type mockUserRepo struct {
	users   map[uint64]*User
	nextID  uint64
	created []*User
	deleted []uint64
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[uint64]*User), nextID: 1}
}

func (m *mockUserRepo) Create(_ context.Context, u *User) error {
	for _, existing := range m.users {
		if existing.Email == u.Email {
			return apperr.Wrap(apperr.CodeConflict, "user already exists", nil)
		}
	}
	u.ID = m.nextID
	m.nextID++
	m.users[u.ID] = u
	m.created = append(m.created, u)
	return nil
}

func (m *mockUserRepo) FindByID(_ context.Context, id uint64) (*User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, apperr.ErrNotFound
}

func (m *mockUserRepo) FindByEmail(_ context.Context, email string) (*User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (m *mockUserRepo) List(_ context.Context, offset, limit int, keyword string) ([]User, int64, error) {
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		if keyword != "" && !strings.Contains(u.Name, keyword) && !strings.Contains(u.Email, keyword) {
			continue
		}
		out = append(out, *u)
	}
	total := int64(len(out))
	switch {
	case offset > len(out):
		out = nil
	case offset+limit < len(out):
		out = out[offset : offset+limit]
	default:
		out = out[offset:]
	}
	return out, total, nil
}

func (m *mockUserRepo) Update(_ context.Context, u *User) error {
	m.users[u.ID] = u
	return nil
}

func (m *mockUserRepo) Delete(_ context.Context, id uint64) error {
	if _, ok := m.users[id]; !ok {
		return apperr.ErrNotFound
	}
	delete(m.users, id)
	m.deleted = append(m.deleted, id)
	return nil
}

func TestServiceCreate(t *testing.T) {
	svc := NewService(newMockUserRepo())

	u, err := svc.Create(context.Background(), Input{Name: "Alice", Email: "  Alice@Example.com "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email normalized to %q, want alice@example.com", u.Email)
	}
	if u.Status != StatusActive {
		t.Errorf("status = %d, want active", u.Status)
	}

	// Duplicate email must be rejected with a conflict error.
	_, err = svc.Create(context.Background(), Input{Name: "Alice2", Email: "alice@example.com"})
	if !errors.Is(err, apperr.ErrConflict) {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestServiceGetNotFound(t *testing.T) {
	svc := NewService(newMockUserRepo())
	_, err := svc.Get(context.Background(), 42)
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestServiceUpdate(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewService(repo)

	created, _ := svc.Create(context.Background(), Input{Name: "Bob", Email: "bob@example.com"})
	name := "Bobby"
	u, err := svc.Update(context.Background(), created.ID, Patch{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if u.Name != "Bobby" {
		t.Errorf("name = %q, want Bobby", u.Name)
	}
}

func TestServiceDelete(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewService(repo)

	created, _ := svc.Create(context.Background(), Input{Name: "Cara", Email: "cara@example.com"})
	if err := svc.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(context.Background(), created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("second delete should be not found, got %v", err)
	}
}
