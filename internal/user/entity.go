// Package user is the user domain module: entity, persistence, use-cases and
// the HTTP adapter live together in one package. Files separate the layers
// (entity.go / repository.go / service.go / handler.go / routes.go) without
// paying for four packages per domain.
package user

import (
	"time"

	"github.com/example/go-web-template/internal/platform/authkit"
)

// Status mirrors the users.status column.
type Status int8

const (
	// StatusActive marks an enabled account.
	StatusActive Status = 1
	// StatusDisabled marks a disabled account.
	StatusDisabled Status = 0
)

// User is the user aggregate root.
//
// PasswordHash is never serialized to JSON and never copied into UserDTO: only
// the auth domain reads it, through the repository.
type User struct {
	ID     uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name   string       `gorm:"type:varchar(64);not null" json:"name"`
	Email  string       `gorm:"type:varchar(255);not null;uniqueIndex" json:"email"`
	Status Status       `gorm:"type:smallint;not null;default:1" json:"status"`
	Role   authkit.Role `gorm:"type:varchar(32);not null;default:member" json:"role"`

	// PasswordHash holds the bcrypt digest. Empty means "no credentials yet",
	// such an account cannot log in until an admin sets a password.
	PasswordHash      string     `gorm:"type:varchar(255);not null;default:''" json:"-"`
	TokenVersion      int        `gorm:"type:integer;not null;default:0" json:"-"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty"`
	PasswordChangedAt *time.Time `json:"-"`
	FailedLoginCount  int16      `gorm:"column:failed_login_attempts;type:smallint;not null;default:0" json:"-"`
	LockedUntil       *time.Time `json:"-"`

	CreatedAt time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time  `gorm:"not null" json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (User) TableName() string { return "users" }

// IsActive reports whether the account may authenticate.
func (u *User) IsActive() bool { return u.Status == StatusActive }
