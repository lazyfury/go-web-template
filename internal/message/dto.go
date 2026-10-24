package message

import (
	"encoding/json"
	"time"

	"github.com/example/go-web-template/internal/platform/authkit"
)

// SendRequest is the payload for POST /messages.
type SendRequest struct {
	Title       string            `json:"title" binding:"max=128"`
	Body        string            `json:"body" binding:"max=20000"`
	Type        Type              `json:"type" binding:"omitempty,oneof=system announcement alert"`
	Level       Level             `json:"level" binding:"omitempty,oneof=info success warning error"`
	Link        string            `json:"link" binding:"max=512"`
	Channels    []Channel         `json:"channels" binding:"required,min=1"`
	Recipients  RecipientsRequest `json:"recipients" binding:"required"`
	TemplateKey string            `json:"template_key" binding:"omitempty,max=64"`
	Locale      string            `json:"locale" binding:"omitempty,max=16"`
	Data        map[string]any    `json:"data"`
	ExpiresAt   *time.Time        `json:"expires_at"`
}

// RecipientsRequest selects the audience; exactly one mode is allowed.
type RecipientsRequest struct {
	All     bool           `json:"all"`
	UserIDs []uint64       `json:"user_ids"`
	Roles   []authkit.Role `json:"roles"`
}

// SendResultDTO is returned by POST /messages.
type SendResultDTO struct {
	MessageID      uint64 `json:"message_id"`
	RecipientCount int    `json:"recipient_count"`
	DeliveryCount  int    `json:"delivery_count"`
}

// InboxItemDTO is one inbox entry.
type InboxItemDTO struct {
	ID        uint64     `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Type      Type       `json:"type" enum:"system,announcement,alert"`
	Level     Level      `json:"level" enum:"info,success,warning,error"`
	Link      string     `json:"link"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// InboxResponse is the payload for GET /messages/inbox.
type InboxResponse struct {
	Items    []InboxItemDTO `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// UnreadCountResponse is the payload for GET /messages/unread-count.
type UnreadCountResponse struct {
	Count int64 `json:"count"`
}

// SentItemDTO is one row of the admin sent list.
type SentItemDTO struct {
	ID             uint64    `json:"id"`
	Title          string    `json:"title"`
	Type           Type      `json:"type" enum:"system,announcement,alert"`
	Level          Level     `json:"level" enum:"info,success,warning,error"`
	Audience       Audience  `json:"audience" enum:"users,all,role"`
	RecipientCount int64     `json:"recipient_count"`
	ReadCount      int64     `json:"read_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// SentListResponse is the payload for GET /messages/sent.
type SentListResponse struct {
	Items    []SentItemDTO `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// CreateTemplateRequest is the payload for POST /message-templates.
type CreateTemplateRequest struct {
	Key       string   `json:"key" binding:"required,max=64"`
	Channel   Channel  `json:"channel" binding:"required,oneof=inbox email sms webhook"`
	Locale    string   `json:"locale" binding:"omitempty,max=16"`
	Subject   string   `json:"subject" binding:"max=255"`
	Body      string   `json:"body" binding:"required"`
	Variables []string `json:"variables"`
	Status    *Status  `json:"status" binding:"omitempty,oneof=0 1"`
}

// UpdateTemplateRequest is the payload for PATCH /message-templates/:id.
// Key/channel/locale are immutable: they are the identity of a template.
type UpdateTemplateRequest struct {
	Subject   *string   `json:"subject" binding:"omitempty,max=255"`
	Body      *string   `json:"body" binding:"omitempty,min=1"`
	Variables *[]string `json:"variables"`
	Status    *Status   `json:"status" binding:"omitempty,oneof=0 1"`
}

// TemplateDTO is the HTTP representation of a template.
type TemplateDTO struct {
	ID        uint64    `json:"id"`
	Key       string    `json:"key"`
	Channel   Channel   `json:"channel" enum:"inbox,email,sms,webhook"`
	Locale    string    `json:"locale"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Variables []string  `json:"variables"`
	Status    Status    `json:"status" enum:"0,1"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TemplateListResponse is the payload for GET /message-templates.
type TemplateListResponse struct {
	Items    []TemplateDTO `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// DeliveryDTO is the HTTP representation of one external delivery.
type DeliveryDTO struct {
	ID                uint64         `json:"id"`
	MessageID         uint64         `json:"message_id"`
	RecipientID       *uint64        `json:"recipient_id,omitempty"`
	Channel           Channel        `json:"channel" enum:"email,sms,webhook"`
	Target            string         `json:"target"`
	Status            DeliveryStatus `json:"status" enum:"0,1,2,3,4,5"`
	Attempts          int            `json:"attempts"`
	MaxAttempts       int            `json:"max_attempts"`
	NextAttemptAt     time.Time      `json:"next_attempt_at"`
	ProviderMessageID string         `json:"provider_message_id"`
	LastError         string         `json:"last_error"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// DeliveryListResponse is the payload for GET /message-deliveries.
type DeliveryListResponse struct {
	Items    []DeliveryDTO `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

func toInboxDTO(item *InboxMessage) InboxItemDTO {
	return InboxItemDTO{
		ID:        item.ID,
		Title:     item.Title,
		Body:      item.Body,
		Type:      item.Type,
		Level:     item.Level,
		Link:      item.Link,
		ReadAt:    item.ReadAt,
		CreatedAt: item.CreatedAt,
	}
}

func toInboxDTOs(items []InboxMessage) []InboxItemDTO {
	out := make([]InboxItemDTO, 0, len(items))
	for i := range items {
		out = append(out, toInboxDTO(&items[i]))
	}
	return out
}

func toSentDTO(item *SentMessage) SentItemDTO {
	return SentItemDTO{
		ID:             item.ID,
		Title:          item.Title,
		Type:           item.Type,
		Level:          item.Level,
		Audience:       item.Audience,
		RecipientCount: item.RecipientCount,
		ReadCount:      item.ReadCount,
		CreatedAt:      item.CreatedAt,
	}
}

func toSentDTOs(items []SentMessage) []SentItemDTO {
	out := make([]SentItemDTO, 0, len(items))
	for i := range items {
		out = append(out, toSentDTO(&items[i]))
	}
	return out
}

func toTemplateDTO(template *Template) TemplateDTO {
	return TemplateDTO{
		ID:        template.ID,
		Key:       template.Key,
		Channel:   template.Channel,
		Locale:    template.Locale,
		Subject:   template.Subject,
		Body:      template.Body,
		Variables: templateVariables(template),
		Status:    template.Status,
		CreatedAt: template.CreatedAt,
		UpdatedAt: template.UpdatedAt,
	}
}

func toTemplateDTOs(items []Template) []TemplateDTO {
	out := make([]TemplateDTO, 0, len(items))
	for i := range items {
		out = append(out, toTemplateDTO(&items[i]))
	}
	return out
}

func templateVariables(template *Template) []string {
	var values []string
	if err := json.Unmarshal([]byte(template.Variables), &values); err != nil {
		return []string{}
	}
	return values
}

func toDeliveryDTO(delivery *Delivery) DeliveryDTO {
	return DeliveryDTO{
		ID:                delivery.ID,
		MessageID:         delivery.MessageID,
		RecipientID:       delivery.RecipientID,
		Channel:           delivery.Channel,
		Target:            delivery.Target,
		Status:            delivery.Status,
		Attempts:          delivery.Attempts,
		MaxAttempts:       delivery.MaxAttempts,
		NextAttemptAt:     delivery.NextAttemptAt,
		ProviderMessageID: delivery.ProviderMessageID,
		LastError:         delivery.LastError,
		CreatedAt:         delivery.CreatedAt,
		UpdatedAt:         delivery.UpdatedAt,
	}
}

func toDeliveryDTOs(items []Delivery) []DeliveryDTO {
	out := make([]DeliveryDTO, 0, len(items))
	for i := range items {
		out = append(out, toDeliveryDTO(&items[i]))
	}
	return out
}
