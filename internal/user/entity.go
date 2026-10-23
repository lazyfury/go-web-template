// Package user is the user domain module: entity, persistence, use-cases and
// the HTTP adapter live together in one package. Files separate the layers
// (entity.go / repository.go / service.go / handler.go / routes.go) without
// paying for four packages per domain.
package user

import "time"

// Status mirrors the users.status column.
type Status int8

const (
	// StatusActive marks an enabled account.
	StatusActive Status = 1
	// StatusDisabled marks a disabled account.
	StatusDisabled Status = 0
)

// User is the user aggregate root.
type User struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string     `gorm:"type:varchar(64);not null" json:"name"`
	Email     string     `gorm:"type:varchar(255);not null;uniqueIndex" json:"email"`
	Status    Status     `gorm:"type:smallint;not null;default:1" json:"status"`
	CreatedAt time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time  `gorm:"not null" json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (User) TableName() string { return "users" }
