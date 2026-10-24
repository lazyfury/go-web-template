// Package auth is the authentication domain: login, access tokens, refresh
// token rotation, logout and password changes. It owns credentials, while the
// user rows themselves stay in the user domain (accessed through UserStore).
package auth

import "time"

// RefreshToken is a persisted refresh-token session. Only the SHA-256 digest of
// the token is stored, so a database leak does not expose usable tokens.
type RefreshToken struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64     `gorm:"not null;index" json:"user_id"`
	TokenHash string     `gorm:"column:token_hash;type:char(64);not null;uniqueIndex" json:"-"`
	ExpiresAt time.Time  `gorm:"not null;index" json:"expires_at"`
	RevokedAt *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	// RevokedReason distinguishes "rotated" (grace window applies) from
	// logout/password/reuse/session-cap (never tolerated).
	RevokedReason string `gorm:"type:varchar(32)" json:"-"`
	// SessionStartedAt is copied across rotations and caps the whole session.
	SessionStartedAt time.Time `gorm:"not null" json:"-"`
	UserAgent        string    `gorm:"type:varchar(255)" json:"-"`
	IP               string    `gorm:"type:varchar(64)" json:"-"`
	CreatedAt        time.Time `gorm:"not null" json:"created_at"`
}

// TableName keeps the table name explicit.
func (RefreshToken) TableName() string { return "refresh_tokens" }

// Revocation reasons. Only RevokeRotated may be replayed inside the reuse
// interval; every other reason takes effect immediately.
const (
	// RevokeRotated marks a token replaced by a fresh one during rotation.
	RevokeRotated = "rotated"
	// RevokeLogout marks an explicit logout.
	RevokeLogout = "logout"
	// RevokeReuse marks tokens revoked because a replay was detected.
	RevokeReuse = "reuse"
	// RevokeSuperseded marks sibling tokens cleaned up by a tolerated replay.
	RevokeSuperseded = "superseded"
	// RevokePassword marks tokens revoked by a password change or reset.
	RevokePassword = "password"
	// RevokeAdmin marks tokens revoked by an administrator action (reset or
	// disable through the users API).
	RevokeAdmin = "admin"
	// RevokeSessionCap marks tokens revoked because the session hit its cap.
	RevokeSessionCap = "session_cap"
)

// Active reports whether the session can still be exchanged for a new token.
func (t *RefreshToken) Active(now time.Time) bool {
	return t.RevokedAt == nil && now.Before(t.ExpiresAt)
}
