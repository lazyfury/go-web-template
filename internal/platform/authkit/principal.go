// Package authkit provides the transport-agnostic authentication primitives:
// JWT access-token issuing/parsing, the authenticated principal, the RBAC
// table and the gin middleware that guards routes.
//
// It is a leaf package: it must not import any business domain. Anything that
// needs to load a user (status, token version) is injected through the
// Verifier interface, implemented by the auth service in the composition root.
package authkit

// Role is a coarse-grained authorization role stored on the user row.
type Role string

const (
	// RoleAdmin can manage every resource.
	RoleAdmin Role = "admin"
	// RoleMember can only read and update their own profile.
	RoleMember Role = "member"
)

// Valid reports whether the role is one of the supported roles.
func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleMember
}

// Permission is a fine-grained capability checked by the middleware.
type Permission string

const (
	// PermUsersRead allows listing and reading any user.
	PermUsersRead Permission = "users:read"
	// PermUsersWrite allows creating and updating any user.
	PermUsersWrite Permission = "users:write"
	// PermUsersDelete allows deleting any user.
	PermUsersDelete Permission = "users:delete"
	// PermUsersReadSelf allows reading the caller's own profile.
	PermUsersReadSelf Permission = "users:read:self"
	// PermUsersWriteSelf allows updating the caller's own profile.
	PermUsersWriteSelf Permission = "users:write:self"
	// PermDictsRead allows reading enabled dictionary groups/items.
	PermDictsRead Permission = "dicts:read"
	// PermDictsWrite allows managing dictionary groups/items.
	PermDictsWrite Permission = "dicts:write"
)

// Principal is the authenticated caller extracted from a valid access token.
type Principal struct {
	UserID       uint64
	Role         Role
	TokenVersion int
}

// IsAdmin reports whether the caller holds the admin role.
func (p Principal) IsAdmin() bool { return p.Role == RoleAdmin }
