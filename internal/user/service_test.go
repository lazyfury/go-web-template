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
	return NewService(repo, passwd.NewHasher(4), &fakeRevoker{}, nil)
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
	if _, _, _, _, err := svc.List(context.Background(), adminActor, 1, 20, ""); err != nil {
		t.Fatalf("admin list: %v", err)
	}
	if _, _, _, _, err := svc.List(context.Background(), memberA, 1, 20, ""); !errors.Is(err, apperr.ErrForbidden) {
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
	svc := NewService(repo, passwd.NewHasher(4), revoker, nil)

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
	svc := NewService(newMockUserRepo(), passwd.NewHasher(4), revoker, nil)
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

// TestServiceListNormalizesPaging 钉住 F12：service 归一化后的分页值要能被 handler
// 回显（否则响应里写着 page_size=100000，实际只读了 20 行）。
func TestServiceListNormalizesPaging(t *testing.T) {
	svc := newTestService(newMockUserRepo())
	cases := []struct{ page, size, wantPage, wantSize int }{
		{1, 20, 1, 20},
		{0, 0, 1, 20},
		{-3, 100000, 1, 20},
		{2, 500, 2, 20},
		{3, 50, 3, 50},
	}
	for _, tc := range cases {
		_, _, page, size, err := svc.List(context.Background(), adminActor, tc.page, tc.size, "")
		if err != nil {
			t.Fatalf("List(%d,%d): %v", tc.page, tc.size, err)
		}
		if page != tc.wantPage || size != tc.wantSize {
			t.Errorf("List(%d,%d) normalized to (%d,%d), want (%d,%d)",
				tc.page, tc.size, page, size, tc.wantPage, tc.wantSize)
		}
	}
}

// TestServiceMemberCannotChangeEmail 钉住 F9：成员改自己邮箱曾返回 409
// "email already registered"，等于给所有已登录用户一个邮箱枚举接口。
func TestServiceMemberCannotChangeEmail(t *testing.T) {
	repo := newMockUserRepo()
	svc := newTestService(repo)
	self := authkit.Principal{UserID: memberA.UserID, Role: authkit.RoleMember}
	existing := "taken@example.com"
	free := "free@example.com"

	created, err := svc.Create(context.Background(), &Input{
		Name: "Member", Email: "member-self@example.com", Password: "password-1234", Role: authkit.RoleMember,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(context.Background(), &Input{
		Name: "Taken", Email: existing, Password: "password-1234", Role: authkit.RoleMember,
	}); err != nil {
		t.Fatalf("Create taken: %v", err)
	}

	// 无论目标邮箱是否已注册，成员都只能拿到 403（不可区分）。
	for _, email := range []string{existing, free} {
		_, err := svc.Update(context.Background(), self, created.ID, Patch{Email: &email})
		if !errors.Is(err, apperr.ErrForbidden) {
			t.Errorf("member changing email to %q = %v, want forbidden", email, err)
		}
	}
	// 管理员仍可改（此时 409 才有意义）。
	if _, err := svc.Update(context.Background(), adminActor, created.ID, Patch{Email: &existing}); !errors.Is(err, apperr.ErrConflict) {
		t.Errorf("admin duplicate email = %v, want conflict", err)
	}
}

type fakeInvalidator struct{ invalidated []uint64 }

func (f *fakeInvalidator) InvalidateAuthState(userID uint64) {
	f.invalidated = append(f.invalidated, userID)
}

// TestServiceInvalidatesCachedAuthState pins every write that can change what
// the access-token check sees. A missing call here would leave a disabled or
// deleted user authenticated for one cache TTL.
func TestServiceInvalidatesCachedAuthState(t *testing.T) {
	repo := newMockUserRepo()
	invalidator := &fakeInvalidator{}
	svc := NewService(repo, passwd.NewHasher(4), &fakeRevoker{}, invalidator)

	created, err := svc.Create(context.Background(), &Input{Name: "Ivy", Email: "ivy@example.com", Password: "secret-pass"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A role change does not revoke sessions, but it does change the cached role.
	member := authkit.RoleMember
	if _, err := svc.Update(context.Background(), adminActor, created.ID, Patch{Role: &member}); err != nil {
		t.Fatalf("Update role: %v", err)
	}
	// A profile-only edit leaves the auth state intact; invalidating anyway is
	// the cheap, impossible-to-forget choice.
	name := "Ivy Renamed"
	if _, err := svc.Update(context.Background(), adminActor, created.ID, Patch{Name: &name}); err != nil {
		t.Fatalf("Update name: %v", err)
	}
	if err := svc.Delete(context.Background(), adminActor, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(invalidator.invalidated) != 3 {
		t.Fatalf("invalidated = %v, want one entry per write", invalidator.invalidated)
	}
	for _, id := range invalidator.invalidated {
		if id != created.ID {
			t.Errorf("invalidated user %d, want %d", id, created.ID)
		}
	}
}
