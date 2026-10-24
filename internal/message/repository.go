package message

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/apperr"
)

// DeliveryDraft describes an external delivery before the message and its
// recipients have IDs. RecipientIndex points into the recipients slice; use -1
// for deliveries that are not tied to one inbox recipient (for example a
// message-level webhook).
type DeliveryDraft struct {
	RecipientIndex int
	Channel        Channel
	Target         string
	Payload        string
	MaxAttempts    int
	DedupeKey      string
}

// InboxMessage is one row of a user's inbox: the message joined with the
// recipient's read state.
type InboxMessage struct {
	Message
	RecipientID uint64     `gorm:"column:recipient_id" json:"recipient_id"`
	ReadAt      *time.Time `gorm:"column:read_at" json:"read_at,omitempty"`
}

// SentMessage is one row of the admin sent list with read statistics.
type SentMessage struct {
	Message
	RecipientCount int64 `gorm:"column:recipient_count" json:"recipient_count"`
	ReadCount      int64 `gorm:"column:read_count" json:"read_count"`
}

// Repository is the persistence contract for the message module.
type Repository interface {
	CreateMessage(ctx context.Context, message *Message, recipients []Recipient, deliveries []DeliveryDraft, now time.Time) error
	FindMessageByID(ctx context.Context, id uint64) (*Message, error)
	ListInbox(ctx context.Context, userID uint64, offset, limit int, unreadOnly bool) ([]InboxMessage, int64, error)
	CountUnread(ctx context.Context, userID uint64) (int64, error)
	FindRecipient(ctx context.Context, userID, messageID uint64) (*Recipient, error)
	MarkRead(ctx context.Context, userID, messageID uint64, now time.Time) error
	MarkAllRead(ctx context.Context, userID uint64, now time.Time) error
	DeleteRecipient(ctx context.Context, userID, messageID uint64) error
	ListSent(ctx context.Context, offset, limit int) ([]SentMessage, int64, error)
	CountRecipients(ctx context.Context, messageID uint64) (total, read int64, err error)

	FindDeliveryByID(ctx context.Context, id uint64) (*Delivery, error)
	ClaimDeliveries(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]Delivery, error)
	MarkDeliverySent(ctx context.Context, id uint64, providerMessageID string, now time.Time) error
	MarkDeliveryFailed(ctx context.Context, id uint64, reason string, nextAttempt time.Time, dead bool, now time.Time) error
	ListDeliveries(ctx context.Context, offset, limit int, status *DeliveryStatus, channel *Channel) ([]Delivery, int64, error)
	RetryDelivery(ctx context.Context, id uint64, now time.Time) error

	CreateTemplate(ctx context.Context, template *Template) error
	FindTemplateByID(ctx context.Context, id uint64) (*Template, error)
	FindTemplateByKey(ctx context.Context, key string, channel Channel, locale string) (*Template, error)
	ListTemplates(ctx context.Context, offset, limit int) ([]Template, int64, error)
	UpdateTemplate(ctx context.Context, template *Template) error
	DeleteTemplate(ctx context.Context, id uint64) error
}

type gormRepository struct {
	db *gorm.DB
}

// NewRepository creates a GORM-backed Repository.
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) CreateMessage(ctx context.Context, message *Message, recipients []Recipient, drafts []DeliveryDraft, now time.Time) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		if len(recipients) > 0 {
			for i := range recipients {
				recipients[i].MessageID = message.ID
			}
			if err := tx.CreateInBatches(&recipients, 500).Error; err != nil {
				return err
			}
		}
		if len(drafts) == 0 {
			return nil
		}

		deliveries := make([]Delivery, 0, len(drafts))
		for _, draft := range drafts {
			var recipientID *uint64
			if draft.RecipientIndex >= 0 {
				if draft.RecipientIndex >= len(recipients) {
					return apperr.Wrap(apperr.CodeInvalidArgument, "delivery references an unknown recipient", nil)
				}
				id := recipients[draft.RecipientIndex].ID
				recipientID = &id
			}
			maxAttempts := draft.MaxAttempts
			if maxAttempts <= 0 {
				maxAttempts = 5
			}
			deliveries = append(deliveries, Delivery{
				MessageID:     message.ID,
				RecipientID:   recipientID,
				Channel:       draft.Channel,
				Target:        draft.Target,
				Payload:       draft.Payload,
				Status:        DeliveryPending,
				MaxAttempts:   maxAttempts,
				NextAttemptAt: now,
				DedupeKey:     draft.DedupeKey,
			})
		}
		if err := tx.CreateInBatches(&deliveries, 500).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "message delivery already exists", err)
		}
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return err
		}
		return apperr.Wrap(apperr.CodeDatabase, "create message failed", err)
	}
	return nil
}

func (r *gormRepository) FindMessageByID(ctx context.Context, id uint64) (*Message, error) {
	var message Message
	err := r.db.WithContext(ctx).First(&message, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find message failed", err)
	}
	return &message, nil
}

func (r *gormRepository) inboxQuery(ctx context.Context, userID uint64, unreadOnly bool) *gorm.DB {
	query := r.db.WithContext(ctx).
		Table("messages").
		Joins("JOIN message_recipients ON message_recipients.message_id = messages.id").
		Where("message_recipients.user_id = ?", userID).
		Where("message_recipients.deleted_at IS NULL").
		Where("messages.deleted_at IS NULL")
	if unreadOnly {
		query = query.Where("message_recipients.read_at IS NULL")
	}
	return query
}

func (r *gormRepository) ListInbox(ctx context.Context, userID uint64, offset, limit int, unreadOnly bool) ([]InboxMessage, int64, error) {
	var total int64
	if err := r.inboxQuery(ctx, userID, unreadOnly).Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count inbox failed", err)
	}

	var items []InboxMessage
	err := r.inboxQuery(ctx, userID, unreadOnly).
		Select("messages.*, message_recipients.id AS recipient_id, message_recipients.read_at").
		Order("messages.created_at DESC, messages.id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&items).Error
	if err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list inbox failed", err)
	}
	return items, total, nil
}

func (r *gormRepository) CountUnread(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	err := r.inboxQuery(ctx, userID, true).Count(&count).Error
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeDatabase, "count unread messages failed", err)
	}
	return count, nil
}

func (r *gormRepository) FindRecipient(ctx context.Context, userID, messageID uint64) (*Recipient, error) {
	var recipient Recipient
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND message_id = ?", userID, messageID).
		First(&recipient).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find message recipient failed", err)
	}
	return &recipient, nil
}

func (r *gormRepository) MarkRead(ctx context.Context, userID, messageID uint64, now time.Time) error {
	recipient, err := r.FindRecipient(ctx, userID, messageID)
	if err != nil {
		return err
	}
	if recipient.ReadAt != nil {
		return nil
	}
	err = r.db.WithContext(ctx).Model(&Recipient{}).
		Where("id = ?", recipient.ID).
		Update("read_at", now).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "mark message read failed", err)
	}
	return nil
}

func (r *gormRepository) MarkAllRead(ctx context.Context, userID uint64, now time.Time) error {
	err := r.db.WithContext(ctx).Model(&Recipient{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", now).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "mark all messages read failed", err)
	}
	return nil
}

func (r *gormRepository) DeleteRecipient(ctx context.Context, userID, messageID uint64) error {
	recipient, err := r.FindRecipient(ctx, userID, messageID)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Delete(&Recipient{}, recipient.ID)
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "delete message recipient failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *gormRepository) ListSent(ctx context.Context, offset, limit int) ([]SentMessage, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&Message{}).Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count sent messages failed", err)
	}

	var items []SentMessage
	err := r.db.WithContext(ctx).
		Table("messages").
		Select(`messages.*,
			(SELECT COUNT(*) FROM message_recipients
			 WHERE message_id = messages.id AND deleted_at IS NULL) AS recipient_count,
			(SELECT COUNT(*) FROM message_recipients
			 WHERE message_id = messages.id AND read_at IS NOT NULL AND deleted_at IS NULL) AS read_count`).
		Where("messages.deleted_at IS NULL").
		Order("messages.id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&items).Error
	if err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list sent messages failed", err)
	}
	return items, total, nil
}

func (r *gormRepository) CountRecipients(ctx context.Context, messageID uint64) (total, read int64, err error) {
	query := r.db.WithContext(ctx).Model(&Recipient{}).Where("message_id = ?", messageID)
	if err := query.Count(&total).Error; err != nil {
		return 0, 0, apperr.Wrap(apperr.CodeDatabase, "count message recipients failed", err)
	}
	if err := r.db.WithContext(ctx).Model(&Recipient{}).
		Where("message_id = ? AND read_at IS NOT NULL", messageID).
		Count(&read).Error; err != nil {
		return 0, 0, apperr.Wrap(apperr.CodeDatabase, "count read recipients failed", err)
	}
	return total, read, nil
}

func (r *gormRepository) FindDeliveryByID(ctx context.Context, id uint64) (*Delivery, error) {
	var delivery Delivery
	err := r.db.WithContext(ctx).First(&delivery, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find delivery failed", err)
	}
	return &delivery, nil
}

func (r *gormRepository) ClaimDeliveries(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]Delivery, error) {
	if limit <= 0 {
		return nil, nil
	}
	var claimed []Delivery
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []Delivery
		err := tx.Where(
			"(status = ? AND next_attempt_at <= ?) OR (status = ? AND locked_until IS NOT NULL AND locked_until < ?)",
			DeliveryPending, now, DeliverySending, now,
		).
			Order("next_attempt_at ASC, id ASC").
			Limit(limit).
			Find(&candidates).Error
		if err != nil {
			return err
		}

		for i := range candidates {
			lockedUntil := now.Add(lease)
			res := tx.Model(&Delivery{}).
				Where(
					"id = ? AND ((status = ? AND next_attempt_at <= ?) OR (status = ? AND locked_until IS NOT NULL AND locked_until < ?))",
					candidates[i].ID, DeliveryPending, now, DeliverySending, now,
				).
				Updates(map[string]any{
					"status":       DeliverySending,
					"locked_until": lockedUntil,
					"attempts":     gorm.Expr("attempts + 1"),
					"updated_at":   now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				candidates[i].Status = DeliverySending
				candidates[i].LockedUntil = &lockedUntil
				candidates[i].Attempts++
				claimed = append(claimed, candidates[i])
			}
		}
		return nil
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "claim deliveries failed", err)
	}
	return claimed, nil
}

func (r *gormRepository) MarkDeliverySent(ctx context.Context, id uint64, providerMessageID string, now time.Time) error {
	res := r.db.WithContext(ctx).Model(&Delivery{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":              DeliverySent,
			"provider_message_id": providerMessageID,
			"locked_until":        nil,
			"last_error":          "",
			"next_attempt_at":     now,
			"updated_at":          now,
		})
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "mark delivery sent failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *gormRepository) MarkDeliveryFailed(ctx context.Context, id uint64, reason string, nextAttempt time.Time, dead bool, now time.Time) error {
	status := DeliveryFailed
	if dead {
		status = DeliveryDead
	}
	res := r.db.WithContext(ctx).Model(&Delivery{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":          status,
			"last_error":      reason,
			"next_attempt_at": nextAttempt,
			"locked_until":    nil,
			"updated_at":      now,
		})
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "mark delivery failed failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *gormRepository) ListDeliveries(ctx context.Context, offset, limit int, status *DeliveryStatus, channel *Channel) ([]Delivery, int64, error) {
	query := r.db.WithContext(ctx).Model(&Delivery{})
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if channel != nil {
		query = query.Where("channel = ?", *channel)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count deliveries failed", err)
	}

	var items []Delivery
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list deliveries failed", err)
	}
	return items, total, nil
}

func (r *gormRepository) RetryDelivery(ctx context.Context, id uint64, now time.Time) error {
	delivery, err := r.FindDeliveryByID(ctx, id)
	if err != nil {
		return err
	}
	if delivery.Status != DeliveryFailed && delivery.Status != DeliveryDead {
		return apperr.Wrap(apperr.CodeConflict, "delivery is not retryable", nil)
	}
	err = r.db.WithContext(ctx).Model(&Delivery{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":              DeliveryPending,
			"attempts":            0,
			"next_attempt_at":     now,
			"locked_until":        nil,
			"provider_message_id": "",
			"last_error":          "",
			"updated_at":          now,
		}).Error
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabase, "retry delivery failed", err)
	}
	return nil
}

func (r *gormRepository) CreateTemplate(ctx context.Context, template *Template) error {
	// Select includes zero values so a disabled template stays disabled.
	err := r.db.WithContext(ctx).
		Select("template_key", "channel", "locale", "subject", "body", "variables", "status").
		Create(template).Error
	if err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "message template already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "create message template failed", err)
	}
	return nil
}

func (r *gormRepository) FindTemplateByID(ctx context.Context, id uint64) (*Template, error) {
	var template Template
	err := r.db.WithContext(ctx).First(&template, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find message template failed", err)
	}
	return &template, nil
}

func (r *gormRepository) FindTemplateByKey(ctx context.Context, key string, channel Channel, locale string) (*Template, error) {
	var template Template
	err := r.db.WithContext(ctx).
		Where("template_key = ? AND channel = ? AND locale = ?", key, channel, locale).
		First(&template).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "find message template by key failed", err)
	}
	return &template, nil
}

func (r *gormRepository) ListTemplates(ctx context.Context, offset, limit int) ([]Template, int64, error) {
	query := r.db.WithContext(ctx).Model(&Template{})
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "count message templates failed", err)
	}
	var items []Template
	if err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeDatabase, "list message templates failed", err)
	}
	return items, total, nil
}

func (r *gormRepository) UpdateTemplate(ctx context.Context, template *Template) error {
	err := r.db.WithContext(ctx).
		Model(template).
		Select("template_key", "channel", "locale", "subject", "body", "variables", "status").
		Updates(template).Error
	if err != nil {
		if isUniqueViolation(err) {
			return apperr.Wrap(apperr.CodeConflict, "message template already exists", err)
		}
		return apperr.Wrap(apperr.CodeDatabase, "update message template failed", err)
	}
	return nil
}

func (r *gormRepository) DeleteTemplate(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Delete(&Template{}, id)
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeDatabase, "delete message template failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// isUniqueViolation detects unique-constraint violations across PostgreSQL
// (SQLSTATE 23505) and SQLite ("UNIQUE constraint failed") error shapes.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(fmt.Sprintf("%v", err))
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
}
