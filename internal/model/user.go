// Package model contains the domain models (GORM entities).
package model

import "time"

// UserStatus mirrors the users.status column.
type UserStatus int8

const (
	// UserStatusActive marks an enabled account.
	UserStatusActive UserStatus = 1
	// UserStatusDisabled marks a disabled account.
	UserStatusDisabled UserStatus = 0
)

// User is the user aggregate root.
type User struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string     `gorm:"type:varchar(64);not null" json:"name"`
	Email     string     `gorm:"type:varchar(255);not null;uniqueIndex" json:"email"`
	Status    UserStatus `gorm:"type:smallint;not null;default:1" json:"status"`
	CreatedAt time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time  `gorm:"not null" json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (User) TableName() string { return "users" }
