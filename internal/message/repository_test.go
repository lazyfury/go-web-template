package message

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/testutil"
)

func newTestRepository(t *testing.T) Repository {
	t.Helper()
	db := testutil.NewSQLiteDB(t, &Message{}, &Recipient{}, &Delivery{}, &Template{})
	return NewRepository(db)
}

func TestCreateMessageWithRecipientsAndDeliveries(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	message := &Message{
		Title:    "Maintenance",
		Body:     "Tonight 23:00",
		Type:     TypeAnnouncement,
		Level:    LevelWarning,
		Audience: AudienceUsers,
	}
	recipients := []Recipient{{UserID: 1}, {UserID: 2}}
	drafts := []DeliveryDraft{
		{RecipientIndex: 0, Channel: ChannelEmail, Target: "a@example.com", Payload: `{"subject":"hi"}`, MaxAttempts: 3, DedupeKey: "msg:1:email"},
		{RecipientIndex: 1, Channel: ChannelSMS, Target: "+10000000000", Payload: `{"body":"hi"}`, MaxAttempts: 2, DedupeKey: "msg:1:sms"},
		{RecipientIndex: -1, Channel: ChannelWebhook, Target: "https://example.com/hook", Payload: `{}`, DedupeKey: "msg:1:webhook"},
	}
	if err := repo.CreateMessage(ctx, message, recipients, drafts, now); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if message.ID == 0 {
		t.Fatal("message id should be assigned")
	}
	if recipients[0].ID == 0 || recipients[1].ID == 0 {
		t.Fatal("recipient ids should be assigned")
	}
	if recipients[0].MessageID != message.ID {
		t.Errorf("recipient message id = %d, want %d", recipients[0].MessageID, message.ID)
	}

	deliveries, total, err := repo.ListDeliveries(ctx, 0, 10, nil, nil)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if total != 3 || len(deliveries) != 3 {
		t.Fatalf("deliveries total=%d len=%d, want 3/3", total, len(deliveries))
	}
	for _, delivery := range deliveries {
		if delivery.Status != DeliveryPending {
			t.Errorf("delivery %d status = %d, want pending", delivery.ID, delivery.Status)
		}
		if delivery.MaxAttempts == 0 {
			t.Errorf("delivery %d max attempts should be set", delivery.ID)
		}
		if !delivery.NextAttemptAt.Equal(now) {
			t.Errorf("delivery %d next attempt = %v, want %v", delivery.ID, delivery.NextAttemptAt, now)
		}
	}

	// A duplicate dedupe key must be rejected atomically.
	dup := &Message{Title: "Dup", Body: "x", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	err = repo.CreateMessage(ctx, dup, nil, []DeliveryDraft{
		{RecipientIndex: -1, Channel: ChannelWebhook, Target: "https://example.com/hook", Payload: `{}`, DedupeKey: "msg:1:webhook"},
	}, now)
	if !isCode(err, apperr.CodeConflict) {
		t.Errorf("duplicate dedupe key error = %v, want conflict", err)
	}
}

func TestInboxReadStateAndSentStats(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	first := &Message{Title: "First", Body: "body-1", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	if err := repo.CreateMessage(ctx, first, []Recipient{{UserID: 1}, {UserID: 2}}, nil, now); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := &Message{Title: "Second", Body: "body-2", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	if err := repo.CreateMessage(ctx, second, []Recipient{{UserID: 1}}, nil, now.Add(time.Second)); err != nil {
		t.Fatalf("create second: %v", err)
	}

	items, total, err := repo.ListInbox(ctx, 1, 0, 10, false)
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("inbox total=%d len=%d, want 2/1", total, len(items))
	}
	if items[0].Title != "Second" || items[0].ReadAt != nil {
		t.Errorf("first inbox item = %+v, want newest unread", items[0])
	}

	if count, err := repo.CountUnread(ctx, 1); err != nil || count != 2 {
		t.Errorf("unread count = %d err=%v, want 2", count, err)
	}

	if err := repo.MarkRead(ctx, 1, first.ID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	recipient, err := repo.FindRecipient(ctx, 1, first.ID)
	if err != nil {
		t.Fatalf("FindRecipient: %v", err)
	}
	if recipient.ReadAt == nil {
		t.Error("read_at should be set")
	}
	if count, _ := repo.CountUnread(ctx, 1); count != 1 {
		t.Errorf("unread count after read = %d, want 1", count)
	}
	// Marking read twice is idempotent.
	if err := repo.MarkRead(ctx, 1, first.ID, now.Add(3*time.Second)); err != nil {
		t.Errorf("second MarkRead: %v", err)
	}

	if err := repo.MarkAllRead(ctx, 1, now.Add(4*time.Second)); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	if count, _ := repo.CountUnread(ctx, 1); count != 0 {
		t.Errorf("unread count after read all = %d, want 0", count)
	}

	sent, sentTotal, err := repo.ListSent(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListSent: %v", err)
	}
	if sentTotal != 2 || len(sent) != 2 {
		t.Fatalf("sent total=%d len=%d, want 2/1", sentTotal, len(sent))
	}
	for _, row := range sent {
		if row.Title == "First" {
			if row.RecipientCount != 2 || row.ReadCount != 1 {
				t.Errorf("first stats = recipients %d read %d, want 2/1", row.RecipientCount, row.ReadCount)
			}
		}
	}

	totalRecipients, readRecipients, err := repo.CountRecipients(ctx, first.ID)
	if err != nil || totalRecipients != 2 || readRecipients != 1 {
		t.Errorf("CountRecipients = %d/%d err=%v, want 2/1", totalRecipients, readRecipients, err)
	}

	// A user can delete their own inbox entry; other users keep theirs.
	if err := repo.DeleteRecipient(ctx, 1, first.ID); err != nil {
		t.Fatalf("DeleteRecipient: %v", err)
	}
	if _, err := repo.FindRecipient(ctx, 1, first.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("find deleted recipient = %v, want not found", err)
	}
	if _, err := repo.FindRecipient(ctx, 2, first.ID); err != nil {
		t.Errorf("other user's recipient should still exist: %v", err)
	}
	items, total, _ = repo.ListInbox(ctx, 1, 0, 10, false)
	if total != 1 || len(items) != 1 {
		t.Errorf("inbox after delete total=%d len=%d, want 1/1", total, len(items))
	}

	if err := repo.MarkRead(ctx, 1, 99999, now); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("MarkRead missing = %v, want not found", err)
	}
}

func TestClaimDeliveriesLeaseRetryAndDead(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	message := &Message{Title: "Deliver", Body: "body", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	drafts := []DeliveryDraft{
		{RecipientIndex: -1, Channel: ChannelEmail, Target: "a@example.com", Payload: `{}`, MaxAttempts: 2, DedupeKey: "due"},
		{RecipientIndex: -1, Channel: ChannelEmail, Target: "b@example.com", Payload: `{}`, MaxAttempts: 2, DedupeKey: "future"},
	}
	if err := repo.CreateMessage(ctx, message, nil, drafts, now); err != nil {
		t.Fatalf("create message: %v", err)
	}
	deliveries, _, _ := repo.ListDeliveries(ctx, 0, 10, nil, nil)
	var due, future *Delivery
	for i := range deliveries {
		switch deliveries[i].DedupeKey {
		case "due":
			due = &deliveries[i]
		case "future":
			future = &deliveries[i]
		}
	}
	if due == nil || future == nil {
		t.Fatal("seeded deliveries not found")
	}
	db := repo.(*gormRepository).db
	if err := db.Model(&Delivery{}).Where("id = ?", future.ID).Update("next_attempt_at", now.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimDeliveries(ctx, now, 10, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != due.ID || claimed[0].Attempts != 1 || claimed[0].Status != DeliverySending {
		t.Fatalf("claimed = %+v", claimed)
	}

	// The lease prevents an immediate second claim.
	if claimed, err := repo.ClaimDeliveries(ctx, now.Add(10*time.Second), 10, time.Minute); err != nil || len(claimed) != 0 {
		t.Errorf("claim inside lease = %+v err=%v, want none", claimed, err)
	}

	// After the lease expires, the same row is reclaimable and attempts grows.
	claimed, err = repo.ClaimDeliveries(ctx, now.Add(2*time.Minute), 10, time.Minute)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != due.ID || claimed[0].Attempts != 2 {
		t.Fatalf("reclaimed = %+v", claimed)
	}

	// Attempt 2 of 2 fails: it becomes dead.
	if err := repo.MarkDeliveryFailed(ctx, due.ID, "smtp down", now.Add(3*time.Minute), true, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	stored, err := repo.FindDeliveryByID(ctx, due.ID)
	if err != nil {
		t.Fatalf("find dead: %v", err)
	}
	if stored.Status != DeliveryDead || stored.LastError != "smtp down" {
		t.Errorf("dead delivery = %+v", stored)
	}

	// Manual retry resets attempts and makes it pending again.
	if err := repo.RetryDelivery(ctx, due.ID, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	claimed, err = repo.ClaimDeliveries(ctx, now.Add(4*time.Minute), 10, time.Minute)
	if err != nil {
		t.Fatalf("claim retry: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != due.ID || claimed[0].Attempts != 1 {
		t.Fatalf("retry claimed = %+v", claimed)
	}

	if err := repo.MarkDeliverySent(ctx, due.ID, "provider-1", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	stored, _ = repo.FindDeliveryByID(ctx, due.ID)
	if stored.Status != DeliverySent || stored.ProviderMessageID != "provider-1" || stored.LockedUntil != nil {
		t.Errorf("sent delivery = %+v", stored)
	}
	if err := repo.RetryDelivery(ctx, due.ID, now); !isCode(err, apperr.CodeConflict) {
		t.Errorf("retry sent delivery = %v, want conflict", err)
	}
}

func TestClaimRetriesFailedDelivery(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	message := &Message{Title: "Retry", Body: "body", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	if err := repo.CreateMessage(ctx, message, nil, []DeliveryDraft{
		{RecipientIndex: -1, Channel: ChannelEmail, Target: "a@example.com", Payload: `{}`, MaxAttempts: 3},
	}, now); err != nil {
		t.Fatalf("create message: %v", err)
	}
	claimed, err := repo.ClaimDeliveries(ctx, now, 10, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("first claim = %+v err=%v", claimed, err)
	}
	deliveryID := claimed[0].ID
	nextAttempt := now.Add(30 * time.Second)
	if err := repo.MarkDeliveryFailed(ctx, deliveryID, "temporary", nextAttempt, false, now); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if claimed, err := repo.ClaimDeliveries(ctx, now.Add(10*time.Second), 10, time.Minute); err != nil || len(claimed) != 0 {
		t.Errorf("claim before next attempt = %+v err=%v", claimed, err)
	}
	claimed, err = repo.ClaimDeliveries(ctx, nextAttempt, 10, time.Minute)
	if err != nil {
		t.Fatalf("retry claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != deliveryID || claimed[0].Attempts != 2 {
		t.Fatalf("retry claim = %+v, want attempts 2", claimed)
	}
}

func TestTemplateCRUDSoftDeleteAndDisabledStatus(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	template := &Template{
		Key:       "user.welcome",
		Channel:   ChannelEmail,
		Locale:    "zh-CN",
		Subject:   "Welcome {{.Name}}",
		Body:      "Hello {{.Name}}",
		Variables: `["Name"]`,
		Status:    StatusDisabled,
	}
	if err := repo.CreateTemplate(ctx, template); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if template.Status != StatusDisabled {
		t.Errorf("created template status = %d, want disabled", template.Status)
	}
	stored, err := repo.FindTemplateByKey(ctx, "user.welcome", ChannelEmail, "zh-CN")
	if err != nil {
		t.Fatalf("FindTemplateByKey: %v", err)
	}
	if stored.Status != StatusDisabled {
		t.Errorf("stored template status = %d, want disabled", stored.Status)
	}

	if err := repo.CreateTemplate(ctx, &Template{
		Key: "user.welcome", Channel: ChannelEmail, Locale: "zh-CN", Body: "dup",
	}); !isCode(err, apperr.CodeConflict) {
		t.Errorf("duplicate template error = %v, want conflict", err)
	}

	stored.Status = StatusEnabled
	stored.Subject = "Updated"
	if err := repo.UpdateTemplate(ctx, stored); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	stored, _ = repo.FindTemplateByID(ctx, stored.ID)
	if stored.Status != StatusEnabled || stored.Subject != "Updated" {
		t.Errorf("updated template = %+v", stored)
	}

	list, total, err := repo.ListTemplates(ctx, 0, 10, nil, nil)
	if err != nil || total != 1 || len(list) != 1 {
		t.Errorf("ListTemplates total=%d len=%d err=%v", total, len(list), err)
	}

	if err := repo.DeleteTemplate(ctx, stored.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := repo.FindTemplateByID(ctx, stored.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("find deleted template = %v, want not found", err)
	}
	// The partial unique index allows reusing the key after a soft delete.
	if err := repo.CreateTemplate(ctx, &Template{
		Key: "user.welcome", Channel: ChannelEmail, Locale: "zh-CN", Body: "new", Status: StatusEnabled,
	}); err != nil {
		t.Errorf("recreate template after delete: %v", err)
	}
}

func isCode(err error, code apperr.Code) bool {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

func TestPurgeTerminalDeliveriesKeepsUnfinishedWork(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &Message{}, &Recipient{}, &Delivery{}, &Template{})
	repo := NewRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	message := &Message{Title: "Retention", Body: "body", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	drafts := []DeliveryDraft{
		{RecipientIndex: -1, Channel: ChannelEmail, Target: "sent@example.com", Payload: "{}", DedupeKey: "m:email"},
		{RecipientIndex: -1, Channel: ChannelSMS, Target: "+10000000000", Payload: "{}", DedupeKey: "m:sms"},
		{RecipientIndex: -1, Channel: ChannelWebhook, Target: "https://example.com/h", Payload: "{}", DedupeKey: "m:webhook"},
	}
	if err := repo.CreateMessage(ctx, message, nil, drafts, now); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	deliveries, _, err := repo.ListDeliveries(ctx, 0, 10, nil, nil)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(deliveries) != 3 {
		t.Fatalf("deliveries = %d, want 3", len(deliveries))
	}
	byDedupe := make(map[string]Delivery, len(deliveries))
	for _, delivery := range deliveries {
		byDedupe[delivery.DedupeKey] = delivery
	}
	if err := repo.MarkDeliverySent(ctx, byDedupe["m:email"].ID, "provider-1", now); err != nil {
		t.Fatalf("MarkDeliverySent: %v", err)
	}
	if err := repo.MarkDeliveryFailed(ctx, byDedupe["m:sms"].ID, "boom", now.Add(time.Hour), true, now); err != nil {
		t.Fatalf("MarkDeliveryFailed: %v", err)
	}
	// The dead row was just updated; the sent row is old enough to prune.
	old := now.Add(-45 * 24 * time.Hour)
	if err := db.Model(&Delivery{}).Where("id = ?", byDedupe["m:email"].ID).Update("updated_at", old).Error; err != nil {
		t.Fatalf("backdate delivery: %v", err)
	}

	deleted, err := repo.PurgeTerminalDeliveries(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("PurgeTerminalDeliveries: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the old sent row)", deleted)
	}

	remaining, total, err := repo.ListDeliveries(ctx, 0, 10, nil, nil)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if total != 2 {
		t.Fatalf("remaining deliveries = %d, want 2 (dead + pending)", total)
	}
	for _, delivery := range remaining {
		if delivery.DedupeKey == "m:email" {
			t.Error("sent delivery survived the purge")
		}
	}
}

func TestPurgeExpiredMessagesCascadesOnlyExpiredOnes(t *testing.T) {
	db := testutil.NewSQLiteDB(t, &Message{}, &Recipient{}, &Delivery{}, &Template{})
	repo := NewRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	expired := now.Add(-100 * 24 * time.Hour)
	create := func(title string, expiresAt *time.Time, recipients []Recipient, drafts []DeliveryDraft) *Message {
		t.Helper()
		message := &Message{
			Title:     title,
			Body:      "body",
			Type:      TypeSystem,
			Level:     LevelInfo,
			Audience:  AudienceUsers,
			ExpiresAt: expiresAt,
		}
		if err := repo.CreateMessage(ctx, message, recipients, drafts, now); err != nil {
			t.Fatalf("CreateMessage(%s): %v", title, err)
		}
		return message
	}

	oldMessage := create("old", &expired,
		[]Recipient{{UserID: 1}},
		[]DeliveryDraft{{RecipientIndex: 0, Channel: ChannelEmail, Target: "a@example.com", Payload: "{}", DedupeKey: "old:email"}},
	)
	liveMessage := create("live", ptrTime(now.Add(24*time.Hour)), []Recipient{{UserID: 1}}, nil)
	foreverMessage := create("forever", nil, []Recipient{{UserID: 2}}, nil)

	// A soft-deleted message past its expiry must be pruned as well: retention is
	// about the data, not about the UI flag.
	removed := create("removed", &expired, []Recipient{{UserID: 2}}, nil)
	if err := db.Delete(&Message{}, removed.ID).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	deleted, err := repo.PurgeExpiredMessages(ctx, now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("PurgeExpiredMessages: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (expired + soft-deleted expired)", deleted)
	}

	var messages, recipients, deliveries int64
	db.Model(&Message{}).Count(&messages)
	db.Model(&Recipient{}).Count(&recipients)
	db.Model(&Delivery{}).Count(&deliveries)
	if messages != 2 {
		t.Errorf("messages = %d, want 2 (live + never-expiring)", messages)
	}
	if recipients != 2 {
		t.Errorf("recipients = %d, want 2 (only the surviving messages' inbox rows)", recipients)
	}
	if deliveries != 0 {
		t.Errorf("deliveries = %d, want 0 (the expired message's delivery)", deliveries)
	}

	for _, id := range []uint64{liveMessage.ID, foreverMessage.ID} {
		if _, err := repo.FindMessageByID(ctx, id); err != nil {
			t.Errorf("FindMessageByID(%d) = %v, want it kept", id, err)
		}
	}
	if _, err := repo.FindMessageByID(ctx, oldMessage.ID); err == nil {
		t.Error("expired message survived the purge")
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

// TestRenewLeaseOnlyWhileOwned covers the reclaim guard: renewal extends the
// lease only while the row still carries the value this process claimed.
func TestRenewLeaseOnlyWhileOwned(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	msg := &Message{Title: "Renew", Body: "body", Type: TypeSystem, Level: LevelInfo, Audience: AudienceUsers}
	drafts := []DeliveryDraft{{RecipientIndex: -1, Channel: ChannelEmail, Target: "a@example.com", Payload: `{}`, MaxAttempts: 3, DedupeKey: "renew"}}
	if err := repo.CreateMessage(ctx, msg, nil, drafts, now); err != nil {
		t.Fatalf("create message: %v", err)
	}
	claimed, err := repo.ClaimDeliveries(ctx, now, 10, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %+v err=%v", claimed, err)
	}
	delivery := claimed[0]
	if delivery.LockedUntil == nil {
		t.Fatal("claimed delivery has no lease")
	}

	renewedAt := now.Add(30 * time.Second)
	if err := repo.RenewLease(ctx, delivery.ID, delivery.LockedUntil, 5*time.Minute, renewedAt); err != nil {
		t.Fatalf("renew: %v", err)
	}
	stored, err := repo.FindDeliveryByID(ctx, delivery.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	wantUntil := renewedAt.Add(5 * time.Minute).Truncate(time.Second)
	if stored.LockedUntil == nil || !stored.LockedUntil.Equal(wantUntil) {
		t.Fatalf("locked_until = %v, want %v", stored.LockedUntil, wantUntil)
	}
	if stored.Status != DeliverySending || stored.Attempts != 1 {
		t.Errorf("renew must not change status/attempts: %+v", stored)
	}

	// A stale lease value means another worker reclaimed the row in the
	// meantime: the renewal must not resurrect ownership.
	stale := wantUntil.Add(-time.Minute)
	if err := repo.RenewLease(ctx, delivery.ID, &stale, time.Minute, now.Add(2*time.Minute)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("renew with stale lease = %v, want ErrLeaseLost", err)
	}

	// A finished row is not renewable either, even with the right lease value.
	if err := repo.MarkDeliverySent(ctx, delivery.ID, "provider", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if err := repo.RenewLease(ctx, delivery.ID, nil, time.Minute, now.Add(4*time.Minute)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("renew sent delivery = %v, want ErrLeaseLost", err)
	}
}
