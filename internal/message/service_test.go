package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
)

type fakeRepository struct {
	Repository

	createdMessage *Message
	recipients     []Recipient
	drafts         []DeliveryDraft
	templates      map[string]*Template
	recipientState map[uint64]*Recipient
	inbox          []InboxMessage
	sent           []SentMessage
	deliveries     []Delivery
	nextID         uint64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		templates:      make(map[string]*Template),
		recipientState: make(map[uint64]*Recipient),
		nextID:         1,
	}
}

func (r *fakeRepository) CreateMessage(_ context.Context, message *Message, recipients []Recipient, drafts []DeliveryDraft, _ time.Time) error {
	message.ID = r.nextID
	r.nextID++
	message.CreatedAt = time.Now()
	r.createdMessage = message
	r.recipients = append([]Recipient(nil), recipients...)
	r.drafts = append([]DeliveryDraft(nil), drafts...)
	return nil
}

func (r *fakeRepository) CreateTemplate(_ context.Context, template *Template) error {
	for _, existing := range r.templates {
		if existing.Key == template.Key && existing.Channel == template.Channel && existing.Locale == template.Locale {
			return apperr.Wrap(apperr.CodeConflict, "template already exists", nil)
		}
	}
	template.ID = r.nextID
	r.nextID++
	r.templates[templateKey(template.Key, template.Channel, template.Locale)] = template
	return nil
}

func (r *fakeRepository) FindTemplateByID(_ context.Context, id uint64) (*Template, error) {
	for _, template := range r.templates {
		if template.ID == id {
			copied := *template
			return &copied, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (r *fakeRepository) FindTemplateByKey(_ context.Context, key string, channel Channel, locale string) (*Template, error) {
	template, ok := r.templates[templateKey(key, channel, locale)]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	copied := *template
	return &copied, nil
}

func (r *fakeRepository) UpdateTemplate(_ context.Context, template *Template) error {
	r.templates[templateKey(template.Key, template.Channel, template.Locale)] = template
	return nil
}

func (r *fakeRepository) DeleteTemplate(_ context.Context, id uint64) error {
	for key, template := range r.templates {
		if template.ID == id {
			delete(r.templates, key)
			return nil
		}
	}
	return apperr.ErrNotFound
}

func (r *fakeRepository) FindMessageByID(_ context.Context, id uint64) (*Message, error) {
	for i := range r.inbox {
		if r.inbox[i].ID == id {
			copied := r.inbox[i].Message
			return &copied, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (r *fakeRepository) ListInbox(_ context.Context, userID uint64, offset, limit int, unreadOnly bool) ([]InboxMessage, int64, error) {
	return r.inbox, int64(len(r.inbox)), nil
}

func (r *fakeRepository) CountUnread(_ context.Context, userID uint64) (int64, error) {
	return int64(len(r.inbox)), nil
}

func (r *fakeRepository) FindRecipient(_ context.Context, userID, messageID uint64) (*Recipient, error) {
	recipient, ok := r.recipientState[messageID]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	copied := *recipient
	return &copied, nil
}

func (r *fakeRepository) MarkRead(_ context.Context, userID, messageID uint64, now time.Time) error {
	recipient, ok := r.recipientState[messageID]
	if !ok {
		return apperr.ErrNotFound
	}
	recipient.ReadAt = &now
	return nil
}

func (r *fakeRepository) MarkAllRead(_ context.Context, userID uint64, now time.Time) error {
	for _, recipient := range r.recipientState {
		recipient.ReadAt = &now
	}
	return nil
}

func (r *fakeRepository) DeleteRecipient(_ context.Context, userID, messageID uint64) error {
	if _, ok := r.recipientState[messageID]; !ok {
		return apperr.ErrNotFound
	}
	delete(r.recipientState, messageID)
	return nil
}

func (r *fakeRepository) ListSent(_ context.Context, offset, limit int) ([]SentMessage, int64, error) {
	return r.sent, int64(len(r.sent)), nil
}

func (r *fakeRepository) ListDeliveries(_ context.Context, offset, limit int, status *DeliveryStatus, channel *Channel) ([]Delivery, int64, error) {
	return r.deliveries, int64(len(r.deliveries)), nil
}

func (r *fakeRepository) RetryDelivery(_ context.Context, id uint64, now time.Time) error {
	return nil
}

func templateKey(key string, channel Channel, locale string) string {
	return fmt.Sprintf("%s|%s|%s", key, channel, locale)
}

type fakeDirectory struct {
	contacts []Contact
	err      error
}

func (d fakeDirectory) Resolve(_ context.Context, _ RecipientFilter) ([]Contact, error) {
	return d.contacts, d.err
}

func newTestService(repo Repository, directory Directory, opts Options) *Service {
	return NewService(repo, directory, opts)
}

var admin = authkit.Principal{UserID: 99, Role: authkit.RoleAdmin}
var member = authkit.Principal{UserID: 7, Role: authkit.RoleMember}

func TestSendInboxOnly(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, fakeDirectory{contacts: []Contact{{UserID: 7}, {UserID: 8}}}, Options{})

	result, err := svc.Send(context.Background(), admin, &SendInput{
		Title:      "Hello",
		Body:       "World",
		Type:       TypeAnnouncement,
		Level:      LevelWarning,
		Channels:   []Channel{ChannelInbox},
		Recipients: RecipientFilter{UserIDs: []uint64{7, 8}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.MessageID == 0 || result.RecipientCount != 2 || result.DeliveryCount != 0 {
		t.Fatalf("result = %+v", result)
	}
	if repo.createdMessage.Title != "Hello" || repo.createdMessage.Audience != AudienceUsers || repo.createdMessage.CreatedBy == nil || *repo.createdMessage.CreatedBy != 99 {
		t.Errorf("message = %+v", repo.createdMessage)
	}
	if len(repo.recipients) != 2 || repo.recipients[0].UserID != 7 {
		t.Errorf("recipients = %+v", repo.recipients)
	}
	if len(repo.drafts) != 0 {
		t.Errorf("drafts = %+v, want none", repo.drafts)
	}
}

func TestSendMultiChannelCreatesDrafts(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, fakeDirectory{contacts: []Contact{
		{UserID: 7, Email: "a@example.com", Phone: "+100"},
	}}, Options{WebhookURLs: []string{"https://example.com/hook"}})

	result, err := svc.Send(context.Background(), admin, &SendInput{
		Title:      "Hello",
		Body:       "World",
		Channels:   []Channel{ChannelInbox, ChannelEmail, ChannelSMS, ChannelWebhook},
		Recipients: RecipientFilter{UserIDs: []uint64{7}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.RecipientCount != 1 || result.DeliveryCount != 3 {
		t.Fatalf("result = %+v", result)
	}
	if len(repo.drafts) != 3 {
		t.Fatalf("drafts = %+v", repo.drafts)
	}
	targets := map[Channel]string{}
	for _, draft := range repo.drafts {
		targets[draft.Channel] = draft.Target
	}
	if targets[ChannelEmail] != "a@example.com" || targets[ChannelSMS] != "+100" || targets[ChannelWebhook] != "https://example.com/hook" {
		t.Errorf("targets = %+v", targets)
	}
	// Email/SMS are linked to the inbox recipient; webhook is message-level.
	for _, draft := range repo.drafts {
		if draft.Channel == ChannelWebhook {
			if draft.RecipientIndex != -1 {
				t.Errorf("webhook recipient index = %d, want -1", draft.RecipientIndex)
			}
		} else if draft.RecipientIndex != 0 {
			t.Errorf("%s recipient index = %d, want 0", draft.Channel, draft.RecipientIndex)
		}
	}
}

func TestSendTemplateRenderingAndValidation(t *testing.T) {
	repo := newFakeRepository()
	repo.templates[templateKey("user.welcome", ChannelInbox, "zh-CN")] = &Template{
		ID:        1,
		Key:       "user.welcome",
		Channel:   ChannelInbox,
		Locale:    "zh-CN",
		Subject:   "Hi {{.Name}}",
		Body:      "Welcome {{.Name}}",
		Variables: `["Name"]`,
		Status:    StatusEnabled,
	}
	svc := newTestService(repo, fakeDirectory{contacts: []Contact{{UserID: 7}}}, Options{})

	_, err := svc.Send(context.Background(), admin, &SendInput{
		Channels:    []Channel{ChannelInbox},
		Recipients:  RecipientFilter{UserIDs: []uint64{7}},
		TemplateKey: "user.welcome",
		Locale:      "zh-CN",
		Data:        map[string]any{"Name": "Mia"},
	})
	if err != nil {
		t.Fatalf("Send template: %v", err)
	}
	if repo.createdMessage.Title != "Hi Mia" || repo.createdMessage.Body != "Welcome Mia" {
		t.Errorf("rendered message = %+v", repo.createdMessage)
	}

	if _, err := svc.Send(context.Background(), admin, &SendInput{
		Channels:    []Channel{ChannelInbox},
		Recipients:  RecipientFilter{UserIDs: []uint64{7}},
		TemplateKey: "user.welcome",
		Data:        map[string]any{"Other": "x"},
	}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("unknown variable error = %v", err)
	}

	if _, err := svc.Send(context.Background(), admin, &SendInput{
		Channels:    []Channel{ChannelInbox},
		Recipients:  RecipientFilter{UserIDs: []uint64{7}},
		TemplateKey: "missing.template",
	}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("missing template error = %v", err)
	}
}

func TestSendRejectsInvalidAudienceAndChannels(t *testing.T) {
	repo := newFakeRepository()
	directory := fakeDirectory{contacts: []Contact{{UserID: 7}}}
	svc := newTestService(repo, directory, Options{})

	cases := []SendInput{
		{Title: "x", Body: "y", Recipients: RecipientFilter{All: true}},
		{Title: "x", Body: "y", Channels: []Channel{ChannelInbox}, Recipients: RecipientFilter{All: true, UserIDs: []uint64{7}}},
		{Title: "x", Body: "y", Channels: []Channel{ChannelInbox}, Recipients: RecipientFilter{}},
		{Title: "x", Body: "y", Channels: []Channel{ChannelWebhook}, Recipients: RecipientFilter{All: true}},
	}
	for i, input := range cases {
		if _, err := svc.Send(context.Background(), admin, &input); !isCode(err, apperr.CodeInvalidArgument) {
			t.Errorf("case %d error = %v, want invalid argument", i, err)
		}
	}

	// Email requested but no contact has an address.
	if _, err := svc.Send(context.Background(), admin, &SendInput{
		Title: "x", Body: "y", Channels: []Channel{ChannelEmail}, Recipients: RecipientFilter{All: true},
	}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("no email error = %v", err)
	}
}

func TestInboxAndAdminAuthorization(t *testing.T) {
	repo := newFakeRepository()
	repo.inbox = []InboxMessage{{Message: Message{ID: 1, Title: "Hello"}}}
	repo.recipientState[1] = &Recipient{ID: 1, MessageID: 1, UserID: 7}
	repo.sent = []SentMessage{{Message: Message{ID: 1, Title: "Hello"}}}
	svc := newTestService(repo, fakeDirectory{}, Options{})

	items, total, _, _, err := svc.Inbox(context.Background(), member, 1, 20, false)
	if err != nil || total != 1 || len(items) != 1 {
		t.Errorf("inbox = %+v total=%d err=%v", items, total, err)
	}
	if count, err := svc.UnreadCount(context.Background(), member); err != nil || count != 1 {
		t.Errorf("unread = %d err=%v", count, err)
	}
	if _, err := svc.GetInboxMessage(context.Background(), member, 1); err != nil {
		t.Errorf("get inbox: %v", err)
	}
	if err := svc.MarkRead(context.Background(), member, 1); err != nil {
		t.Errorf("mark read: %v", err)
	}
	if repo.recipientState[1].ReadAt == nil {
		t.Error("read_at not set")
	}

	if _, _, _, _, err := svc.ListSent(context.Background(), member, 1, 20); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member sent list = %v, want forbidden", err)
	}
	if err := svc.RetryDelivery(context.Background(), member, 1); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member retry = %v, want forbidden", err)
	}
	if _, err := svc.CreateTemplate(context.Background(), member, &TemplateInput{Key: "x.y", Channel: ChannelInbox, Body: "x", Status: StatusEnabled}); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member create template = %v, want forbidden", err)
	}
}

func TestTemplateCRUDValidation(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, fakeDirectory{}, Options{})

	template, err := svc.CreateTemplate(context.Background(), admin, &TemplateInput{
		Key: "user.welcome", Channel: ChannelEmail, Locale: "zh-CN",
		Subject: "Hi", Body: "Hello", Variables: []string{"Name"}, Status: StatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if template.ID == 0 {
		t.Fatal("template id should be assigned")
	}
	if got := templateVariables(template); len(got) != 1 || got[0] != "Name" {
		t.Errorf("variables = %+v", got)
	}
	if _, err := svc.CreateTemplate(context.Background(), admin, &TemplateInput{
		Key: "user.welcome", Channel: ChannelEmail, Locale: "zh-CN", Body: "dup", Status: StatusEnabled,
	}); !isCode(err, apperr.CodeConflict) {
		t.Errorf("duplicate template error = %v", err)
	}
	if _, err := svc.CreateTemplate(context.Background(), admin, &TemplateInput{
		Key: "Bad Key", Channel: ChannelEmail, Body: "x", Status: StatusEnabled,
	}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("invalid key error = %v", err)
	}

	subject := "Updated"
	status := StatusDisabled
	updated, err := svc.UpdateTemplate(context.Background(), admin, template.ID, TemplatePatch{Subject: &subject, Status: &status})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.Subject != "Updated" || updated.Status != StatusDisabled {
		t.Errorf("updated template = %+v", updated)
	}
	if err := svc.DeleteTemplate(context.Background(), admin, template.ID); err != nil {
		t.Errorf("DeleteTemplate: %v", err)
	}
}

func TestTemplateVariablesEncoding(t *testing.T) {
	if _, err := encodeVariables([]string{"Name", "Name"}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("duplicate variables error = %v", err)
	}
	if _, err := encodeVariables([]string{" "}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Errorf("empty variable error = %v", err)
	}
	raw, err := encodeVariables([]string{"Name", "Count"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil || len(values) != 2 {
		t.Errorf("encoded variables = %s err=%v", raw, err)
	}
}
