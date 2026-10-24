package authkit

// rolePermissions is the static RBAC table. It is deliberately small: roles
// are coarse and permissions are code constants, so a typo fails at compile
// time. Swap this map for database-backed roles when dynamic permissions are
// actually needed (the Can/Permissions API stays the same).
var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
		PermUsersRead,
		PermUsersWrite,
		PermUsersDelete,
	},
	RoleMember: {
		PermUsersReadSelf,
		PermUsersWriteSelf,
	},
}

// Permissions returns the permissions granted to role. The returned slice is a
// copy; callers may keep or sort it.
func Permissions(role Role) []Permission {
	perms := rolePermissions[role]
	out := make([]Permission, len(perms))
	copy(out, perms)
	return out
}

// Can reports whether role grants permission.
func Can(role Role, permission Permission) bool {
	for _, granted := range rolePermissions[role] {
		if granted == permission {
			return true
		}
	}
	return false
}

// CanAny reports whether role grants at least one of permissions.
func CanAny(role Role, permissions ...Permission) bool {
	for _, permission := range permissions {
		if Can(role, permission) {
			return true
		}
	}
	return false
}
