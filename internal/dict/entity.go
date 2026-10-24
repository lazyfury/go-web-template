// Package dict is the dictionary/settings domain module.
//
// A group is one setting (for example "user.status" or "site.seo"), and its
// items are the dict array consumers read: [{key, label, value, ...}]. Other
// modules use the Store convenience methods (Replace/Get/GetInto) instead of
// touching the tables, so the normalized item representation stays an
// implementation detail.
package dict

import (
	"time"

	"gorm.io/gorm"
)

// Status mirrors the status column shared by groups and items.
type Status int8

const (
	// StatusDisabled hides the row from consumer reads.
	StatusDisabled Status = 0
	// StatusEnabled exposes the row to consumer reads.
	StatusEnabled Status = 1
)

// Valid reports whether the status is one of the supported values.
func (s Status) Valid() bool { return s == StatusDisabled || s == StatusEnabled }

// ValueType tells clients how to decode Item.Value.
type ValueType string

const (
	// ValueTypeString stores a plain string.
	ValueTypeString ValueType = "string"
	// ValueTypeNumber stores a number serialized as text.
	ValueTypeNumber ValueType = "number"
	// ValueTypeBoolean stores "true" or "false".
	ValueTypeBoolean ValueType = "boolean"
	// ValueTypeJSON stores an arbitrary JSON document (object/array).
	ValueTypeJSON ValueType = "json"
)

// Valid reports whether the value type is one of the supported kinds.
func (v ValueType) Valid() bool {
	switch v {
	case ValueTypeString, ValueTypeNumber, ValueTypeBoolean, ValueTypeJSON:
		return true
	default:
		return false
	}
}

// Group is a named setting: the dictionary type that owns an array of items.
//
// Key is the stable machine identifier consumers reference in code, so it is
// immutable after creation. Soft-deleted groups keep their row for audit and
// allow the key to be reused.
type Group struct {
	ID          uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Key         string         `gorm:"column:group_key;type:varchar(64);not null;uniqueIndex:ux_dict_groups_key,where:deleted_at IS NULL" json:"key"`
	Name        string         `gorm:"type:varchar(64);not null" json:"name"`
	Description string         `gorm:"type:varchar(255);not null;default:''" json:"description"`
	Status      Status         `gorm:"type:smallint;not null" json:"status"`
	CreatedAt   time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (Group) TableName() string { return "dict_groups" }

// Enabled reports whether consumers may read the group.
func (g *Group) Enabled() bool { return g.Status == StatusEnabled }

// Item is one element of a group's dict array.
//
// Key is the machine value (often a code such as "1" or "active"); Label is
// what humans see; Value optionally overrides Key when the setting stores a
// payload. An empty Value falls back to Key for convenience.
type Item struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID   uint64         `gorm:"not null;uniqueIndex:ux_dict_items_group_key,where:deleted_at IS NULL" json:"group_id"`
	Key       string         `gorm:"column:item_key;type:varchar(64);not null;uniqueIndex:ux_dict_items_group_key,where:deleted_at IS NULL" json:"key"`
	Label     string         `gorm:"type:varchar(128);not null" json:"label"`
	Value     string         `gorm:"type:varchar(1024);not null;default:''" json:"value"`
	ValueType ValueType      `gorm:"type:varchar(16);not null" json:"value_type"`
	SortOrder int            `gorm:"not null;default:0" json:"sort"`
	Status    Status         `gorm:"type:smallint;not null" json:"status"`
	Remark    string         `gorm:"type:varchar(255);not null;default:''" json:"remark"`
	CreatedAt time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (Item) TableName() string { return "dict_items" }

// Enabled reports whether consumers may see the item.
func (i *Item) Enabled() bool { return i.Status == StatusEnabled }

// EffectiveValue returns Value, falling back to Key when Value is empty.
func (i *Item) EffectiveValue() string {
	if i.Value != "" {
		return i.Value
	}
	return i.Key
}
