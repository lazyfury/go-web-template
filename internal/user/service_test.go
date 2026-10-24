package user

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
)

// mockUserRepo is a hand-written fake implementing Repository.
type mockUserRepo struct {
	users   map[uint64]*User
	nextID  uint64
	created []*User
	deleted []uint64
	// authSaved counts SaveAuthState calls so tests can assert that credential
	// changes are persisted through the explicit-column path.
	authSaved int
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

func (m *mockUserRepo) SaveAuthState(_ context.Context, u *User) error {
	m.authSaved++
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

type fakeRevoker struct{ revoked []uint64 }

func (f *fakeRevoker) RevokeUserSessions(_ context.Context, userID uint64) error {
	f.revoked = append(f.revoked, userID)
	return nil
}

func newTestService(repo Repository) *Service {
	// cost 4 keeps the tests fast; production uses auth.bcrypt_cost (12).
	return NewService(repo, passwd.NewHasher(4), &fakeRevoker{})
}

var (
	adminActor = authkit.Principal{UserID: 100, Role: authkit.RoleAdmin}
	memberID   = uint64(7)
	memberA    = authkit.Principal{UserID: memberID, Role: authkit.RoleMember}
)

func TestServiceCreate(t *testing.T) {
	svc := newTestService(newMockUserRepo())

	u, err := svc.Create(context.Background(), &Input{
		Name: "Alice", Email: "  Alice@Example.com ", Phone: " +10000000000 ", Password: "secret-pass",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email normalized to %q, want alice@example.com", u.Email)
	}
	if u.Phone != "+10000000000" {
		t.Errorf("phone = %q, want trimmed +10000000000", u.Phone)
	}
	if u.Status != StatusActive {
		t.Errorf("status = %d, want active", u.Status)
	}
	if u.Role != authkit.RoleMember {
		t.Errorf("role = %q, want member (default)", u.Role)
	}
	if u.PasswordHash == "" || u.PasswordHash == "secret-pass" {
		t.Errorf("password must be hashed, got %q", u.PasswordHash)
	}

	// Duplicate email must be rejected with a conflict error.
	_, err = svc.Create(context.Background(), &Input{Name: "Alice2", Email: "alice@example.com", Password: "secret-pass"})
	if !errors.Is(err, apperr.ErrConflict) {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestServiceCreateRejectsBadInput(t *testing.T) {
	svc := newTestService(newMockUserRepo())

	_, err := svc.Create(context.Background(), &Input{Name: "A", Email: "a@example.com", Password: "secret-pass", Role: "root"})
	if !errors.Is(err, apperr.ErrInvalidArgument) {
		t.Errorf("invalid role should be CodeInvalidArgument, got %v", err)
	}

	tooLong := strings.Repeat("x", passwd.MaxPasswordBytes+1)
	_, err = svc.Create(context.Background(), &Input{Name: "Long", Email: "long@example.com", Password: tooLong})
	if !errors.Is(err, apperr.ErrInvalidArgument) {
		t.Errorf("oversized password should be CodeInvalidArgument, got %v", err)
	}
}

func TestServiceGetNotFound(t *testing.T) {
	svc := newTestService(newMockUserRepo())
	_, err := svc.Get(context.Background(), adminActor, 42)
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestServiceGetOwnership(t *testing.T) {
	repo := newMockUserRepo()
	svc := newTestService(repo)

	created, err := svc.Create(context.Background(), &Input{Name: "Bob", Email: "bob@example.com", Password: "secret-pass"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	self := authkit.Principal{UserID: created.ID, Role: authkit.RoleMember}
	if _, err := svc.Get(context.Background(), self, created.ID); err != nil {
		t.Errorf("member reading self should be allowed: %v", err)
	}
	other := authkit.Principal{UserID: created.ID + 1, Role: authkit.RoleMember}
	_, err = svc.Get(context.Background(), other, created.ID)
	if !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member reading another user should be forbidden, got %v", err)
	}
}

func TestServiceListAdminOnly(t *testing.T) {
	svc := newTestService(newMockUserRepo())
	if _, _, err := svc.List(context.Background(), adminActor, 1, 20, ""); err != nil {
		t.Fatalf("admin list: %v", err)
	}
	if _, _, err := svc.List(context.Background(), memberA, 1, 20, ""); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member list should be forbidden, got %v", err)
	}
}

func TestServiceUpdate(t *testing.T) {
	repo := newMockUserRepo()
	svc := newTestService(repo)

	created, _ := svc.Create(context.Background(), &Input{Name: "Bob", Email: "bob@example.com", Password: "secret-pass"})
	self := authkit.Principal{UserID: created.ID, Role: authkit.RoleMember}

	name := "Bobby"
	u, err := svc.Update(context.Background(), self, created.ID, Patch{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if u.Name != "Bobby" {
		t.Errorf("name = %q, want Bobby", u.Name)
	}

	phone := " +20000000000 "
	u, err = svc.Update(context.Background(), self, created.ID, Patch{Phone: &phone})
	if err != nil {
		t.Fatalf("update phone: %v", err)
	}
	if u.Phone != "+20000000000" {
		t.Errorf("phone = %q, want trimmed +20000000000", u.Phone)
	}

	// A member cannot escalate their own role.
	admin := authkit.RoleAdmin
	if _, err := svc.Update(context.Background(), self, created.ID, Patch{Role: &admin}); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member role change should be forbidden, got %v", err)
	}

	// An admin can.
	if _, err := svc.Update(context.Background(), adminActor, created.ID, Patch{Role: &admin}); err != nil {
		t.Errorf("admin role change: %v", err)
	}
}

func TestServiceUpdatePasswordBumpsTokenVersion(t *testing.T) {
	repo := newMockUserRepo()
	revoker := &fakeRevoker{}
	svc := NewService(repo, passwd.NewHasher(4), revoker)

	created, _ := svc.Create(context.Background(), &Input{Name: "Dee", Email: "dee@example.com", Password: "secret-pass"})
	password := "new-secret-pass"
	updated, err := svc.Update(context.Background(), adminActor, created.ID, Patch{Password: &password})
	if err != nil {
		t.Fatalf("Update password: %v", err)
	}
	if updated.TokenVersion != 1 {
		t.Errorf("token version = %d, want 1", updated.TokenVersion)
	}
	if !passwd.NewHasher(4).Verify(updated.PasswordHash, password) {
		t.Error("password hash does not match the new password")
	}
	if repo.authSaved == 0 {
		t.Error("auth state must be persisted through SaveAuthState")
	}
	if len(revoker.revoked) != 1 || revoker.revoked[0] != created.ID {
		t.Errorf("admin password reset must revoke refresh sessions, got %v", revoker.revoked)
	}
}

func TestServiceDisableBumpsTokenVersion(t *testing.T) {
	revoker := &fakeRevoker{}
	svc := NewService(newMockUserRepo(), passwd.NewHasher(4), revoker)
	created, _ := svc.Create(context.Background(), &Input{Name: "Eve", Email: "eve@example.com", Password: "secret-pass"})

	disabled := StatusDisabled
	updated, err := svc.Update(context.Background(), adminActor, created.ID, Patch{Status: &disabled})
	if err != nil {
		t.Fatalf("Update status: %v", err)
	}
	if updated.Status != StatusDisabled {
		t.Errorf("status = %d, want disabled", updated.Status)
	}
	if updated.TokenVersion != 1 {
		t.Errorf("disabling must bump token_version, got %d", updated.TokenVersion)
	}
	if len(revoker.revoked) != 1 {
		t.Errorf("disabling must revoke refresh sessions, got %v", revoker.revoked)
	}
}

func TestServiceDelete(t *testing.T) {
	repo := newMockUserRepo()
	svc := newTestService(repo)

	created, _ := svc.Create(context.Background(), &Input{Name: "Cara", Email: "cara@example.com", Password: "secret-pass"})

	if err := svc.Delete(context.Background(), memberA, created.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member delete should be forbidden, got %v", err)
	}
	if err := svc.Delete(context.Background(), adminActor, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(context.Background(), adminActor, created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("second delete should be not found, got %v", err)
	}
}

func TestServiceCannotDeleteSelf(t *testing.T) {
	repo := newMockUserRepo()
	svc := newTestService(repo)

	created, _ := svc.Create(context.Background(), &Input{
		Name: "Root", Email: "root@example.com", Password: "secret-pass", Role: authkit.RoleAdmin,
	})
	actor := authkit.Principal{UserID: created.ID, Role: authkit.RoleAdmin}
	if err := svc.Delete(context.Background(), actor, created.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("self delete should be forbidden, got %v", err)
	}
}
