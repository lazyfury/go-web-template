// Package message is the notification domain module: message content, the
// in-app inbox, external delivery records (outbox) and templates.
//
// Phase 1 only contains entities and the persistence contract. HTTP handlers
// and the background worker are added in later phases; the worker is started
// from cmd/server, never from internal/app.
package message

import (
	"time"

	"gorm.io/gorm"
)

// Type classifies the intent of a message.
type Type string

const (
	// TypeSystem is a platform-generated message (welcome, security alert...).
	TypeSystem Type = "system"
	// TypeAnnouncement is an admin broadcast.
	TypeAnnouncement Type = "announcement"
	// TypeAlert is a high-priority operational alert.
	TypeAlert Type = "alert"
)

// Valid reports whether the type is one of the supported values.
func (t Type) Valid() bool {
	return t == TypeSystem || t == TypeAnnouncement || t == TypeAlert
}

// Level controls the UI tone of a message.
type Level string

const (
	// LevelInfo is a neutral informational message.
	LevelInfo Level = "info"
	// LevelSuccess reports a successful operation.
	LevelSuccess Level = "success"
	// LevelWarning reports a non-fatal problem.
	LevelWarning Level = "warning"
	// LevelError reports a failure.
	LevelError Level = "error"
)

// Valid reports whether the level is one of the supported values.
func (l Level) Valid() bool {
	return l == LevelInfo || l == LevelSuccess || l == LevelWarning || l == LevelError
}

// Audience records how recipients were selected, for auditing.
type Audience string

const (
	// AudienceUsers means an explicit list of users.
	AudienceUsers Audience = "users"
	// AudienceAll means every active user.
	AudienceAll Audience = "all"
	// AudienceRole means users holding a specific role.
	AudienceRole Audience = "role"
)

// Valid reports whether the audience is one of the supported values.
func (a Audience) Valid() bool {
	return a == AudienceUsers || a == AudienceAll || a == AudienceRole
}

// Channel is a delivery channel.
type Channel string

const (
	// ChannelInbox stores the message in the user's in-app inbox.
	ChannelInbox Channel = "inbox"
	// ChannelEmail sends the message by email.
	ChannelEmail Channel = "email"
	// ChannelSMS sends the message by SMS.
	ChannelSMS Channel = "sms"
	// ChannelWebhook POSTs the message to a configured webhook.
	ChannelWebhook Channel = "webhook"
)

// Valid reports whether the channel is one of the supported values.
func (c Channel) Valid() bool {
	return c == ChannelInbox || c == ChannelEmail || c == ChannelSMS || c == ChannelWebhook
}

// External reports whether the channel needs a delivery record and a worker.
func (c Channel) External() bool {
	return c == ChannelEmail || c == ChannelSMS || c == ChannelWebhook
}

// Status is the enabled/disabled state of a template.
type Status int8

const (
	// StatusDisabled hides the template from senders.
	StatusDisabled Status = 0
	// StatusEnabled makes the template selectable by senders.
	StatusEnabled Status = 1
)

// Valid reports whether the status is one of the supported values.
func (s Status) Valid() bool { return s == StatusDisabled || s == StatusEnabled }

// DeliveryStatus is the lifecycle of one external delivery attempt series.
type DeliveryStatus int8

const (
	// DeliveryPending is ready to be claimed by a worker.
	DeliveryPending DeliveryStatus = 0
	// DeliverySending is claimed and currently in flight.
	DeliverySending DeliveryStatus = 1
	// DeliverySent finished successfully.
	DeliverySent DeliveryStatus = 2
	// DeliveryFailed failed but may be retried.
	DeliveryFailed DeliveryStatus = 3
	// DeliveryDead exhausted its retry budget.
	DeliveryDead DeliveryStatus = 4
	// DeliveryCanceled was canceled by an operator or because the message was revoked.
	DeliveryCanceled DeliveryStatus = 5
)

// Message is the immutable content snapshot of one notification.
//
// Title/Body are already rendered: templates are applied before insert so later
// template edits never rewrite history.
type Message struct {
	ID          uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Title       string         `gorm:"type:varchar(128);not null" json:"title"`
	Body        string         `gorm:"type:text;not null" json:"body"`
	Type        Type           `gorm:"type:varchar(32);not null" json:"type"`
	Level       Level          `gorm:"type:varchar(16);not null" json:"level"`
	Link        string         `gorm:"type:varchar(512);not null;default:''" json:"link"`
	Audience    Audience       `gorm:"type:varchar(16);not null" json:"audience"`
	TemplateKey string         `gorm:"type:varchar(64);not null;default:''" json:"template_key"`
	CreatedBy   *uint64        `json:"created_by,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	CreatedAt   time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (Message) TableName() string { return "messages" }

// Recipient is one user's inbox entry for a message.
type Recipient struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MessageID uint64         `gorm:"not null;uniqueIndex:ux_message_recipients_message_user" json:"message_id"`
	UserID    uint64         `gorm:"not null;uniqueIndex:ux_message_recipients_message_user" json:"user_id"`
	ReadAt    *time.Time     `json:"read_at,omitempty"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt time.Time      `gorm:"not null" json:"created_at"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (Recipient) TableName() string { return "message_recipients" }

// Delivery is one external-channel delivery job. It doubles as the outbox:
// the worker claims pending rows, sends them, and records the result.
type Delivery struct {
	ID                uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MessageID         uint64         `gorm:"not null;index" json:"message_id"`
	RecipientID       *uint64        `gorm:"index" json:"recipient_id,omitempty"`
	Channel           Channel        `gorm:"type:varchar(16);not null" json:"channel"`
	Target            string         `gorm:"type:varchar(512);not null" json:"target"`
	Payload           string         `gorm:"type:text;not null" json:"payload"`
	Status            DeliveryStatus `gorm:"type:smallint;not null" json:"status"`
	Attempts          int            `gorm:"not null;default:0" json:"attempts"`
	MaxAttempts       int            `gorm:"not null;default:5" json:"max_attempts"`
	NextAttemptAt     time.Time      `gorm:"not null" json:"next_attempt_at"`
	LockedUntil       *time.Time     `json:"locked_until,omitempty"`
	ProviderMessageID string         `gorm:"type:varchar(128);not null;default:''" json:"provider_message_id"`
	LastError         string         `gorm:"type:text;not null;default:''" json:"last_error"`
	DedupeKey         string         `gorm:"type:varchar(128);not null;default:'';uniqueIndex:ux_message_deliveries_dedupe,where:dedupe_key <> ''" json:"dedupe_key"`
	CreatedAt         time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"not null" json:"updated_at"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (Delivery) TableName() string { return "message_deliveries" }

// Template is a reusable subject/body pair for one channel and locale.
//
// Variables is a JSON array of allowed variable names, stored as TEXT so the
// same schema works on PostgreSQL and SQLite.
type Template struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Key       string         `gorm:"column:template_key;type:varchar(64);not null;uniqueIndex:ux_message_templates_key,where:deleted_at IS NULL" json:"key"`
	Channel   Channel        `gorm:"type:varchar(16);not null;uniqueIndex:ux_message_templates_key,where:deleted_at IS NULL" json:"channel"`
	Locale    string         `gorm:"type:varchar(16);not null;default:'zh-CN';uniqueIndex:ux_message_templates_key,where:deleted_at IS NULL" json:"locale"`
	Subject   string         `gorm:"type:varchar(255);not null;default:''" json:"subject"`
	Body      string         `gorm:"type:text;not null" json:"body"`
	Variables string         `gorm:"type:text;not null;default:'[]'" json:"variables"`
	Status    Status         `gorm:"type:smallint;not null" json:"status"`
	CreatedAt time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName keeps the table name explicit even if the struct is renamed.
func (Template) TableName() string { return "message_templates" }

// Enabled reports whether a sender may use the template.
func (t *Template) Enabled() bool { return t.Status == StatusEnabled }
