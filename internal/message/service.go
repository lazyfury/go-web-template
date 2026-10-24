package message

import (
	"bytes"
	"context"
	"encoding/json"
	htmltemplate "html/template"
	"net/url"
	"regexp"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode/utf8"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxTitleRunes   = 128
	maxBodyRunes    = 20000
	maxLinkRunes    = 512
	maxSubjectRunes = 255
	maxLocaleRunes  = 16
)

// templateKeyPattern keeps template keys stable and URL-safe.
var templateKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// Contact is one resolved recipient with the contact details needed by the
// external channels.
type Contact struct {
	UserID uint64
	Email  string
	Phone  string
}

// RecipientFilter selects the audience of a message. Exactly one mode is
// allowed: All, UserIDs, or Roles.
type RecipientFilter struct {
	All     bool
	UserIDs []uint64
	Roles   []authkit.Role
}

// Directory resolves a recipient filter into active contacts. The composition
// root adapts the user repository to this interface, keeping the message
// domain independent of the user domain.
type Directory interface {
	Resolve(ctx context.Context, filter RecipientFilter) ([]Contact, error)
}

// Options configures delivery behavior.
type Options struct {
	// WebhookURLs is the allowlist used by the webhook channel; empty disables
	// the channel.
	WebhookURLs []string
	// DefaultLocale is used when a send request does not provide a locale.
	DefaultLocale string
	// MaxAttempts is the retry budget stored on each external delivery.
	MaxAttempts int
	// Now is injectable for tests.
	Now func() time.Time
}

// Service implements message use-cases.
type Service struct {
	repo          Repository
	directory     Directory
	webhookURLs   []string
	defaultLocale string
	maxAttempts   int
	now           func() time.Time
}

// NewService creates a message Service.
func NewService(repo Repository, directory Directory, opts Options) *Service {
	locale := strings.TrimSpace(opts.DefaultLocale)
	if locale == "" {
		locale = "zh-CN"
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		repo:          repo,
		directory:     directory,
		webhookURLs:   append([]string(nil), opts.WebhookURLs...),
		defaultLocale: locale,
		maxAttempts:   maxAttempts,
		now:           now,
	}
}

// SendInput is the validated input for creating and delivering a message.
type SendInput struct {
	Title       string
	Body        string
	Type        Type
	Level       Level
	Link        string
	Channels    []Channel
	Recipients  RecipientFilter
	TemplateKey string
	Locale      string
	Data        map[string]any
	ExpiresAt   *time.Time
}

// SendResult reports what was created.
type SendResult struct {
	MessageID      uint64
	RecipientCount int
	DeliveryCount  int
}

// TemplateInput is the input for creating a message template.
type TemplateInput struct {
	Key       string
	Channel   Channel
	Locale    string
	Subject   string
	Body      string
	Variables []string
	Status    Status
}

// TemplatePatch is a partial template update; nil means "not provided".
type TemplatePatch struct {
	Subject   *string
	Body      *string
	Variables *[]string
	Status    *Status
}

// Send creates a message, its inbox recipients and external delivery outbox
// rows in one repository transaction. External sends happen in the worker.
func (s *Service) Send(ctx context.Context, actor authkit.Principal, in *SendInput) (*SendResult, error) {
	channels, err := normalizeChannels(in.Channels)
	if err != nil {
		return nil, err
	}
	if err := validateRecipientFilter(in.Recipients); err != nil {
		return nil, err
	}
	contacts, err := s.directory.Resolve(ctx, in.Recipients)
	if err != nil {
		return nil, err
	}
	if len(contacts) == 0 {
		return nil, invalid("no active recipients matched")
	}

	messageType := in.Type
	if messageType == "" {
		messageType = TypeSystem
	}
	if !messageType.Valid() {
		return nil, invalid("invalid message type")
	}
	level := in.Level
	if level == "" {
		level = LevelInfo
	}
	if !level.Valid() {
		return nil, invalid("invalid message level")
	}
	link := strings.TrimSpace(in.Link)
	if utf8.RuneCountInString(link) > maxLinkRunes {
		return nil, invalid("message link is too long")
	}

	locale := normalizeLocale(in.Locale)
	if locale == "" {
		locale = s.defaultLocale
	}
	rendered, err := s.render(ctx, in, channels, locale)
	if err != nil {
		return nil, err
	}

	withInbox := containsChannel(channels, ChannelInbox)
	var recipients []Recipient
	if withInbox {
		recipients = make([]Recipient, 0, len(contacts))
		for _, contact := range contacts {
			recipients = append(recipients, Recipient{UserID: contact.UserID})
		}
	}

	drafts, err := s.deliveryDrafts(channels, contacts, withInbox, rendered)
	if err != nil {
		return nil, err
	}

	message := &Message{
		Title:       rendered.title,
		Body:        rendered.body,
		Type:        messageType,
		Level:       level,
		Link:        link,
		Audience:    audienceFor(in.Recipients),
		TemplateKey: strings.TrimSpace(in.TemplateKey),
		ExpiresAt:   in.ExpiresAt,
	}
	if actor.UserID != 0 {
		createdBy := actor.UserID
		message.CreatedBy = &createdBy
	}
	if err := s.repo.CreateMessage(ctx, message, recipients, drafts, s.now()); err != nil {
		return nil, err
	}
	return &SendResult{
		MessageID:      message.ID,
		RecipientCount: len(contacts),
		DeliveryCount:  len(drafts),
	}, nil
}

// Inbox returns the caller's inbox page.
func (s *Service) Inbox(ctx context.Context, actor authkit.Principal, page, pageSize int, unreadOnly bool) (items []InboxMessage, total int64, normalizedPage, normalizedSize int, err error) {
	page, pageSize = normalizePagination(page, pageSize)
	items, total, err = s.repo.ListInbox(ctx, actor.UserID, (page-1)*pageSize, pageSize, unreadOnly)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return items, total, page, pageSize, nil
}

// UnreadCount returns the number of unread inbox entries.
func (s *Service) UnreadCount(ctx context.Context, actor authkit.Principal) (int64, error) {
	return s.repo.CountUnread(ctx, actor.UserID)
}

// GetInboxMessage returns one inbox entry without changing its read state.
func (s *Service) GetInboxMessage(ctx context.Context, actor authkit.Principal, messageID uint64) (*InboxMessage, error) {
	message, err := s.repo.FindMessageByID(ctx, messageID)
	if err != nil {
		return nil, err
	}
	recipient, err := s.repo.FindRecipient(ctx, actor.UserID, messageID)
	if err != nil {
		return nil, err
	}
	return &InboxMessage{
		Message:     *message,
		RecipientID: recipient.ID,
		ReadAt:      recipient.ReadAt,
	}, nil
}

// MarkRead marks one of the caller's inbox entries read.
func (s *Service) MarkRead(ctx context.Context, actor authkit.Principal, messageID uint64) error {
	return s.repo.MarkRead(ctx, actor.UserID, messageID, s.now())
}

// MarkAllRead marks every unread inbox entry read.
func (s *Service) MarkAllRead(ctx context.Context, actor authkit.Principal) error {
	return s.repo.MarkAllRead(ctx, actor.UserID, s.now())
}

// DeleteInbox removes one inbox entry for the caller (soft delete).
func (s *Service) DeleteInbox(ctx context.Context, actor authkit.Principal, messageID uint64) error {
	return s.repo.DeleteRecipient(ctx, actor.UserID, messageID)
}

// ListSent returns the admin sent list with read statistics.
func (s *Service) ListSent(ctx context.Context, actor authkit.Principal, page, pageSize int) (items []SentMessage, total int64, normalizedPage, normalizedSize int, err error) {
	if !actor.IsAdmin() {
		return nil, 0, 0, 0, forbidden("insufficient permissions")
	}
	page, pageSize = normalizePagination(page, pageSize)
	items, total, err = s.repo.ListSent(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return items, total, page, pageSize, nil
}

// ListDeliveries returns external delivery records for the admin UI.
func (s *Service) ListDeliveries(ctx context.Context, actor authkit.Principal, page, pageSize int, status *DeliveryStatus, channel *Channel) (items []Delivery, total int64, normalizedPage, normalizedSize int, err error) {
	if !actor.IsAdmin() {
		return nil, 0, 0, 0, forbidden("insufficient permissions")
	}
	page, pageSize = normalizePagination(page, pageSize)
	items, total, err = s.repo.ListDeliveries(ctx, (page-1)*pageSize, pageSize, status, channel)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return items, total, page, pageSize, nil
}

// RetryDelivery re-queues a failed or dead delivery.
func (s *Service) RetryDelivery(ctx context.Context, actor authkit.Principal, id uint64) error {
	if !actor.IsAdmin() {
		return forbidden("insufficient permissions")
	}
	return s.repo.RetryDelivery(ctx, id, s.now())
}

// CreateTemplate validates and creates a template.
func (s *Service) CreateTemplate(ctx context.Context, actor authkit.Principal, in *TemplateInput) (*Template, error) {
	if !actor.IsAdmin() {
		return nil, forbidden("insufficient permissions")
	}
	template, err := buildTemplate(in, s.defaultLocale)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateTemplate(ctx, template); err != nil {
		return nil, err
	}
	return template, nil
}

// GetTemplate returns a template by id.
func (s *Service) GetTemplate(ctx context.Context, actor authkit.Principal, id uint64) (*Template, error) {
	if !actor.IsAdmin() {
		return nil, forbidden("insufficient permissions")
	}
	return s.repo.FindTemplateByID(ctx, id)
}

// ListTemplates returns a template page.
func (s *Service) ListTemplates(ctx context.Context, actor authkit.Principal, page, pageSize int, channel *Channel, status *Status) (items []Template, total int64, normalizedPage, normalizedSize int, err error) {
	if !actor.IsAdmin() {
		return nil, 0, 0, 0, forbidden("insufficient permissions")
	}
	page, pageSize = normalizePagination(page, pageSize)
	items, total, err = s.repo.ListTemplates(ctx, (page-1)*pageSize, pageSize, channel, status)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return items, total, page, pageSize, nil
}

// UpdateTemplate applies a partial update to subject/body/variables/status.
func (s *Service) UpdateTemplate(ctx context.Context, actor authkit.Principal, id uint64, patch TemplatePatch) (*Template, error) {
	if !actor.IsAdmin() {
		return nil, forbidden("insufficient permissions")
	}
	template, err := s.repo.FindTemplateByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Subject != nil {
		subject := strings.TrimSpace(*patch.Subject)
		if utf8.RuneCountInString(subject) > maxSubjectRunes {
			return nil, invalid("template subject is too long")
		}
		template.Subject = subject
	}
	if patch.Body != nil {
		body := strings.TrimSpace(*patch.Body)
		if body == "" {
			return nil, invalid("template body is required")
		}
		template.Body = body
	}
	if patch.Variables != nil {
		variables, err := encodeVariables(*patch.Variables)
		if err != nil {
			return nil, err
		}
		template.Variables = variables
	}
	if patch.Status != nil {
		if !patch.Status.Valid() {
			return nil, invalid("invalid template status")
		}
		template.Status = *patch.Status
	}
	if err := s.repo.UpdateTemplate(ctx, template); err != nil {
		return nil, err
	}
	return template, nil
}

// DeleteTemplate soft-deletes a template.
func (s *Service) DeleteTemplate(ctx context.Context, actor authkit.Principal, id uint64) error {
	if !actor.IsAdmin() {
		return forbidden("insufficient permissions")
	}
	return s.repo.DeleteTemplate(ctx, id)
}

type renderedContent struct {
	title      string
	body       string
	perChannel map[Channel]deliveryPayload
}

type deliveryPayload struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (p deliveryPayload) json() string {
	raw, _ := json.Marshal(p)
	return string(raw)
}

func (s *Service) render(ctx context.Context, in *SendInput, channels []Channel, locale string) (*renderedContent, error) {
	templateKey := strings.TrimSpace(in.TemplateKey)
	if templateKey == "" {
		title := strings.TrimSpace(in.Title)
		body := strings.TrimSpace(in.Body)
		if title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
			return nil, invalid("message title must be between 1 and 128 characters")
		}
		if body == "" || utf8.RuneCountInString(body) > maxBodyRunes {
			return nil, invalid("message body must be between 1 and 20000 characters")
		}
		payload := deliveryPayload{Subject: title, Body: body}
		perChannel := make(map[Channel]deliveryPayload, len(channels))
		for _, channel := range channels {
			perChannel[channel] = payload
		}
		return &renderedContent{title: title, body: body, perChannel: perChannel}, nil
	}

	if !templateKeyPattern.MatchString(templateKey) {
		return nil, invalid("invalid template key")
	}
	perChannel := make(map[Channel]deliveryPayload, len(channels))
	var canonical *deliveryPayload
	for _, channel := range channels {
		template, err := s.repo.FindTemplateByKey(ctx, templateKey, channel, locale)
		if err != nil {
			if err == apperr.ErrNotFound {
				return nil, invalid("template not found for channel " + string(channel))
			}
			return nil, err
		}
		if !template.Enabled() {
			return nil, invalid("template is disabled for channel " + string(channel))
		}
		if err := checkVariableKeys(template, in.Data); err != nil {
			return nil, err
		}
		subject, err := renderText(template.Key+"-subject", template.Subject, in.Data)
		if err != nil {
			return nil, err
		}
		body, err := renderBody(channel, template.Key+"-body", template.Body, in.Data)
		if err != nil {
			return nil, err
		}
		payload := deliveryPayload{Subject: subject, Body: body}
		perChannel[channel] = payload
		if canonical == nil || channel == ChannelInbox {
			canonical = &payload
		}
	}
	if canonical == nil {
		return nil, invalid("no channels selected")
	}
	title := strings.TrimSpace(canonical.Subject)
	if title == "" {
		title = templateKey
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return nil, invalid("rendered title is too long")
	}
	return &renderedContent{title: title, body: canonical.Body, perChannel: perChannel}, nil
}

func (s *Service) deliveryDrafts(channels []Channel, contacts []Contact, withInbox bool, rendered *renderedContent) ([]DeliveryDraft, error) {
	drafts := make([]DeliveryDraft, 0, len(contacts)+len(s.webhookURLs))
	for _, channel := range channels {
		payload := rendered.perChannel[channel]
		switch channel {
		case ChannelInbox:
			continue
		case ChannelEmail:
			count := 0
			for i, contact := range contacts {
				target := strings.TrimSpace(contact.Email)
				if target == "" {
					continue
				}
				count++
				drafts = append(drafts, DeliveryDraft{
					RecipientIndex: recipientIndex(withInbox, i),
					Channel:        ChannelEmail,
					Target:         target,
					Payload:        payload.json(),
					MaxAttempts:    s.maxAttempts,
				})
			}
			if count == 0 {
				return nil, invalid("no recipients have an email address")
			}
		case ChannelSMS:
			count := 0
			for i, contact := range contacts {
				target := strings.TrimSpace(contact.Phone)
				if target == "" {
					continue
				}
				count++
				drafts = append(drafts, DeliveryDraft{
					RecipientIndex: recipientIndex(withInbox, i),
					Channel:        ChannelSMS,
					Target:         target,
					Payload:        payload.json(),
					MaxAttempts:    s.maxAttempts,
				})
			}
			if count == 0 {
				return nil, invalid("no recipients have a phone number")
			}
		case ChannelWebhook:
			urls, err := s.webhookTargets()
			if err != nil {
				return nil, err
			}
			for _, target := range urls {
				drafts = append(drafts, DeliveryDraft{
					RecipientIndex: -1,
					Channel:        ChannelWebhook,
					Target:         target,
					Payload:        payload.json(),
					MaxAttempts:    s.maxAttempts,
				})
			}
		}
	}
	return drafts, nil
}

func (s *Service) webhookTargets() ([]string, error) {
	if len(s.webhookURLs) == 0 {
		return nil, invalid("webhook channel is not configured")
	}
	targets := make([]string, 0, len(s.webhookURLs))
	for _, raw := range s.webhookURLs {
		target := strings.TrimSpace(raw)
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, invalid("invalid webhook url in configuration")
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func buildTemplate(in *TemplateInput, defaultLocale string) (*Template, error) {
	key := strings.ToLower(strings.TrimSpace(in.Key))
	if !templateKeyPattern.MatchString(key) {
		return nil, invalid("invalid template key: use lowercase letters, digits, '_', '.' or '-'")
	}
	if !in.Channel.Valid() {
		return nil, invalid("invalid template channel")
	}
	locale := normalizeLocale(in.Locale)
	if locale == "" {
		locale = defaultLocale
	}
	subject := strings.TrimSpace(in.Subject)
	if utf8.RuneCountInString(subject) > maxSubjectRunes {
		return nil, invalid("template subject is too long")
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, invalid("template body is required")
	}
	variables, err := encodeVariables(in.Variables)
	if err != nil {
		return nil, err
	}
	status := in.Status
	if !status.Valid() {
		return nil, invalid("invalid template status")
	}
	return &Template{
		Key:       key,
		Channel:   in.Channel,
		Locale:    locale,
		Subject:   subject,
		Body:      body,
		Variables: variables,
		Status:    status,
	}, nil
}

func encodeVariables(values []string) (string, error) {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			return "", invalid("template variable names must not be empty")
		}
		if seen[name] {
			return "", invalid("template variable names must be unique")
		}
		seen[name] = true
		cleaned = append(cleaned, name)
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "encode template variables failed", err)
	}
	return string(raw), nil
}

func checkVariableKeys(template *Template, data map[string]any) error {
	if strings.TrimSpace(template.Variables) == "" {
		return nil
	}
	var allowed []string
	if err := json.Unmarshal([]byte(template.Variables), &allowed); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "stored template variables are invalid", err)
	}
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		set[name] = true
	}
	for key := range data {
		if !set[key] {
			return invalid("template variable not allowed: " + key)
		}
	}
	return nil
}

func renderText(name, source string, data map[string]any) (string, error) {
	tmpl, err := texttemplate.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", invalid("invalid text template: " + err.Error())
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", invalid("render template: " + err.Error())
	}
	return buf.String(), nil
}

func renderBody(channel Channel, name, source string, data map[string]any) (string, error) {
	if channel != ChannelEmail {
		return renderText(name, source, data)
	}
	tmpl, err := htmltemplate.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", invalid("invalid html template: " + err.Error())
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", invalid("render template: " + err.Error())
	}
	return buf.String(), nil
}

func normalizeChannels(values []Channel) ([]Channel, error) {
	if len(values) == 0 {
		return nil, invalid("at least one channel is required")
	}
	out := make([]Channel, 0, len(values))
	seen := make(map[Channel]bool, len(values))
	for _, channel := range values {
		if !channel.Valid() {
			return nil, invalid("invalid channel: " + string(channel))
		}
		if seen[channel] {
			continue
		}
		seen[channel] = true
		out = append(out, channel)
	}
	return out, nil
}

func validateRecipientFilter(filter RecipientFilter) error {
	modes := 0
	if filter.All {
		modes++
	}
	if len(filter.UserIDs) > 0 {
		modes++
	}
	if len(filter.Roles) > 0 {
		modes++
	}
	if modes != 1 {
		return invalid("recipients must specify exactly one of all, user_ids or roles")
	}
	for _, id := range filter.UserIDs {
		if id == 0 {
			return invalid("invalid recipient user id")
		}
	}
	for _, role := range filter.Roles {
		if !role.Valid() {
			return invalid("invalid recipient role")
		}
	}
	return nil
}

func audienceFor(filter RecipientFilter) Audience {
	switch {
	case filter.All:
		return AudienceAll
	case len(filter.Roles) > 0:
		return AudienceRole
	default:
		return AudienceUsers
	}
}

func containsChannel(channels []Channel, want Channel) bool {
	for _, channel := range channels {
		if channel == want {
			return true
		}
	}
	return false
}

func recipientIndex(withInbox bool, contactIndex int) int {
	if withInbox {
		return contactIndex
	}
	return -1
}

func normalizeLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if utf8.RuneCountInString(locale) > maxLocaleRunes {
		return ""
	}
	return locale
}

func normalizePagination(page, size int) (normalizedPage, normalizedSize int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > maxPageSize {
		size = defaultPageSize
	}
	return page, size
}

func invalid(message string) error {
	return apperr.Wrap(apperr.CodeInvalidArgument, message, nil)
}

func forbidden(message string) error {
	return apperr.New(apperr.CodeForbidden, message)
}
