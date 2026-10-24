// Package seed creates deterministic demo data for local and test
// environments. It is used by cmd/seeding and must never run against
// production unless explicitly allowed by the caller.
package seed

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/dict"
	"github.com/example/go-web-template/internal/message"
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
	"github.com/example/go-web-template/internal/user"
)

var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// Seed parts accepted by Options.Only and the seeding subcommands.
const (
	// PartUsers seeds fake users.
	PartUsers = "users"
	// PartDict seeds demo dictionary groups/items.
	PartDict = "dict"
	// PartTemplates seeds demo message templates.
	PartTemplates = "templates"
	// PartMessages seeds inbox messages for seeded users.
	PartMessages = "messages"
)

// Options controls the deterministic fixture set.
type Options struct {
	// Users is the number of fake users to create.
	Users int
	// Messages is the number of inbox messages to create.
	Messages int
	// MaxRecipients caps how many seeded users receive one message.
	MaxRecipients int
	// AdminEvery promotes every Nth seeded user to admin (0 = never).
	AdminEvery int
	// Prefix namespaces emails, message titles and dict/template keys.
	Prefix string
	// Only limits the run to specific parts (users/dict/templates/messages).
	// Empty means all parts.
	Only []string
	// Password is the shared test password (never logged).
	Password string
	// BcryptCost keeps local seeding fast; production users use auth.bcrypt_cost.
	BcryptCost int
	// RandomSeed makes recipient selection reproducible.
	RandomSeed int64
	// Now is injectable for tests.
	Now func() time.Time
}

// Result reports what the run created or skipped.
type Result struct {
	UsersCreated      int
	UsersSkipped      int
	DictGroupsCreated int
	DictGroupsSkipped int
	DictItemsCreated  int
	DictItemsSkipped  int
	TemplatesCreated  int
	TemplatesSkipped  int
	MessagesCreated   int
	MessagesSkipped   int
	RecipientsCreated int
	RecipientsRead    int
}

// Run creates the fixture set. It is idempotent for users, dictionaries and
// templates (matched by stable keys); messages are skipped entirely when any
// "[prefix]" message already exists, so a re-run never duplicates history.
func Run(ctx context.Context, db *gorm.DB, opts *Options) (Result, error) {
	if err := normalizeOptions(opts); err != nil {
		return Result{}, err
	}
	only, err := normalizeOnly(opts.Only)
	if err != nil {
		return Result{}, err
	}
	now := opts.Now()

	userRepo := user.NewRepository(db)
	dictRepo := dict.NewRepository(db)
	messageRepo := message.NewRepository(db)

	var result Result
	var contacts []user.User
	if only[PartUsers] {
		contacts, err = seedUsers(ctx, userRepo, opts, now, &result)
		if err != nil {
			return result, err
		}
	} else {
		contacts, err = loadSeedUsers(ctx, db, opts.Prefix)
		if err != nil {
			return result, err
		}
	}
	if only[PartDict] {
		if err := seedDictionaries(ctx, dictRepo, opts.Prefix, &result); err != nil {
			return result, err
		}
	}
	if only[PartTemplates] {
		if err := seedTemplates(ctx, messageRepo, opts.Prefix, &result); err != nil {
			return result, err
		}
	}
	if only[PartMessages] {
		if len(contacts) == 0 {
			return result, fmt.Errorf("messages require seeded users; run `seeding users` first")
		}
		if err := seedMessages(ctx, db, messageRepo, contacts, opts, now, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func normalizeOnly(values []string) (map[string]bool, error) {
	all := map[string]bool{
		PartUsers:     true,
		PartDict:      true,
		PartTemplates: true,
		PartMessages:  true,
	}
	if len(values) == 0 {
		return all, nil
	}
	out := make(map[string]bool, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			switch part {
			case "":
				continue
			case "user":
				part = PartUsers
			case "dicts", "dictionary", "dictionaries":
				part = PartDict
			case "template":
				part = PartTemplates
			case "message":
				part = PartMessages
			}
			if !all[part] {
				return nil, fmt.Errorf("unknown seed part %q (want users, dict, templates, messages)", part)
			}
			out[part] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no seed parts selected")
	}
	return out, nil
}

func loadSeedUsers(ctx context.Context, db *gorm.DB, prefix string) ([]user.User, error) {
	var contacts []user.User
	like := prefix + "-user-%"
	if err := db.WithContext(ctx).Model(&user.User{}).
		Where("email LIKE ?", like).
		Order("id ASC").
		Find(&contacts).Error; err != nil {
		return nil, fmt.Errorf("load seed users: %w", err)
	}
	return contacts, nil
}

func normalizeOptions(opts *Options) error {
	opts.Prefix = strings.TrimSpace(opts.Prefix)
	if opts.Prefix == "" {
		opts.Prefix = "seed"
	}
	if !prefixPattern.MatchString(opts.Prefix) {
		return fmt.Errorf("prefix must match %s", prefixPattern.String())
	}
	if opts.Users < 0 || opts.Messages < 0 || opts.AdminEvery < 0 {
		return fmt.Errorf("users, messages and admin-every must be >= 0")
	}
	if opts.MaxRecipients <= 0 {
		opts.MaxRecipients = 5
	}
	if opts.BcryptCost == 0 {
		opts.BcryptCost = 4
	}
	if opts.BcryptCost < 4 || opts.BcryptCost > 14 {
		return fmt.Errorf("bcrypt-cost must be between 4 and 14")
	}
	if len(opts.Password) < 8 || len(opts.Password) > passwd.MaxPasswordBytes {
		return fmt.Errorf("password must be between 8 and %d bytes", passwd.MaxPasswordBytes)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return nil
}

func seedUsers(ctx context.Context, repo user.Repository, opts *Options, now time.Time, result *Result) ([]user.User, error) {
	hasher := passwd.NewHasher(opts.BcryptCost)
	contacts := make([]user.User, 0, opts.Users)
	for i := 1; i <= opts.Users; i++ {
		email := fmt.Sprintf("%s-user-%03d@example.test", opts.Prefix, i)
		existing, err := repo.FindByEmail(ctx, email)
		if err == nil {
			contacts = append(contacts, *existing)
			result.UsersSkipped++
			continue
		}
		if !errors.Is(err, apperr.ErrNotFound) {
			return nil, fmt.Errorf("find seed user %s: %w", email, err)
		}

		role := authkit.RoleMember
		if opts.AdminEvery > 0 && i%opts.AdminEvery == 0 {
			role = authkit.RoleAdmin
		}
		hash, err := hasher.Hash(opts.Password)
		if err != nil {
			return nil, fmt.Errorf("hash seed user password: %w", err)
		}
		account := &user.User{
			Name:              fmt.Sprintf("%s User %03d", titlePrefix(opts.Prefix), i),
			Email:             email,
			Phone:             fmt.Sprintf("+86138%08d", i),
			Status:            user.StatusActive,
			Role:              role,
			PasswordHash:      hash,
			PasswordChangedAt: &now,
		}
		if err := repo.Create(ctx, account); err != nil {
			return nil, fmt.Errorf("create seed user %s: %w", email, err)
		}
		contacts = append(contacts, *account)
		result.UsersCreated++
	}
	return contacts, nil
}

func seedDictionaries(ctx context.Context, repo dict.Repository, prefix string, result *Result) error {
	specs := []struct {
		key   string
		name  string
		desc  string
		items []dictItem
	}{
		{
			key:  "demo.status",
			name: "Demo Status",
			desc: "Seeded status dictionary",
			items: []dictItem{
				{key: "active", label: "Active", value: "active", sort: 10},
				{key: "disabled", label: "Disabled", value: "disabled", sort: 20},
				{key: "pending", label: "Pending", value: "pending", sort: 30},
			},
		},
		{
			key:  "demo.priority",
			name: "Demo Priority",
			desc: "Seeded numeric priority dictionary",
			items: []dictItem{
				{key: "low", label: "Low", value: "10", valueType: dict.ValueTypeNumber, sort: 10},
				{key: "medium", label: "Medium", value: "20", valueType: dict.ValueTypeNumber, sort: 20},
				{key: "high", label: "High", value: "30", valueType: dict.ValueTypeNumber, sort: 30},
			},
		},
		{
			key:  "demo.channel",
			name: "Demo Channel",
			desc: "Seeded delivery channel dictionary",
			items: []dictItem{
				{key: "inbox", label: "Inbox", value: "inbox", sort: 10},
				{key: "email", label: "Email", value: "email", sort: 20},
				{key: "sms", label: "SMS", value: "sms", sort: 30},
				{key: "webhook", label: "Webhook", value: "webhook", sort: 40},
			},
		},
	}

	for _, spec := range specs {
		groupKey := prefix + "." + spec.key
		group, err := repo.FindGroupByKey(ctx, groupKey)
		switch {
		case errors.Is(err, apperr.ErrNotFound):
			group = &dict.Group{Key: groupKey, Name: spec.name, Description: spec.desc, Status: dict.StatusEnabled}
			if err := repo.CreateGroup(ctx, group); err != nil {
				return fmt.Errorf("create seed dict group %s: %w", groupKey, err)
			}
			result.DictGroupsCreated++
		case err != nil:
			return fmt.Errorf("find seed dict group %s: %w", groupKey, err)
		default:
			result.DictGroupsSkipped++
		}

		items, err := repo.ListItems(ctx, group.ID, true)
		if err != nil {
			return fmt.Errorf("list seed dict items %s: %w", groupKey, err)
		}
		existing := make(map[string]bool, len(items))
		for i := range items {
			existing[items[i].Key] = true
		}
		for _, item := range spec.items {
			if existing[item.key] {
				result.DictItemsSkipped++
				continue
			}
			valueType := item.valueType
			if valueType == "" {
				valueType = dict.ValueTypeString
			}
			record := &dict.Item{
				GroupID:   group.ID,
				Key:       item.key,
				Label:     item.label,
				Value:     item.value,
				ValueType: valueType,
				SortOrder: item.sort,
				Status:    dict.StatusEnabled,
			}
			if err := repo.CreateItem(ctx, record); err != nil {
				return fmt.Errorf("create seed dict item %s/%s: %w", groupKey, item.key, err)
			}
			result.DictItemsCreated++
		}
	}
	return nil
}

func seedTemplates(ctx context.Context, repo message.Repository, prefix string, result *Result) error {
	key := prefix + ".demo.welcome"
	specs := []struct {
		channel message.Channel
		subject string
		body    string
	}{
		{message.ChannelInbox, "Hi {{.Name}}", "Welcome to the demo, {{.Name}}."},
		{message.ChannelEmail, "Hi {{.Name}}", "<p>Welcome to the demo, <strong>{{.Name}}</strong>.</p>"},
		{message.ChannelSMS, "", "Welcome, {{.Name}}."},
		{message.ChannelWebhook, "", `{"text":"Welcome, {{.Name}}."}`},
	}
	for _, spec := range specs {
		_, err := repo.FindTemplateByKey(ctx, key, spec.channel, "zh-CN")
		if err == nil {
			result.TemplatesSkipped++
			continue
		}
		if !errors.Is(err, apperr.ErrNotFound) {
			return fmt.Errorf("find seed template %s/%s: %w", key, spec.channel, err)
		}
		template := &message.Template{
			Key:       key,
			Channel:   spec.channel,
			Locale:    "zh-CN",
			Subject:   spec.subject,
			Body:      spec.body,
			Variables: `["Name"]`,
			Status:    message.StatusEnabled,
		}
		if err := repo.CreateTemplate(ctx, template); err != nil {
			return fmt.Errorf("create seed template %s/%s: %w", key, spec.channel, err)
		}
		result.TemplatesCreated++
	}
	return nil
}

func seedMessages(ctx context.Context, db *gorm.DB, repo message.Repository, contacts []user.User, opts *Options, now time.Time, result *Result) error {
	if opts.Messages == 0 || len(contacts) == 0 {
		return nil
	}

	var existing int64
	like := fmt.Sprintf("[%s]%%", opts.Prefix)
	if err := db.WithContext(ctx).Model(&message.Message{}).Where("title LIKE ?", like).Count(&existing).Error; err != nil {
		return fmt.Errorf("count seed messages: %w", err)
	}
	if existing > 0 {
		result.MessagesSkipped = opts.Messages
		return nil
	}

	rng := rand.New(rand.NewSource(opts.RandomSeed)) //nolint:gosec // deterministic fixtures
	maxRecipients := opts.MaxRecipients
	if maxRecipients > len(contacts) {
		maxRecipients = len(contacts)
	}
	types := []message.Type{message.TypeSystem, message.TypeAnnouncement, message.TypeAlert}
	levels := []message.Level{message.LevelInfo, message.LevelSuccess, message.LevelWarning, message.LevelError}

	for i := 0; i < opts.Messages; i++ {
		count := 1 + rng.Intn(maxRecipients)
		perm := rng.Perm(len(contacts))
		recipients := make([]message.Recipient, 0, count)
		for _, index := range perm[:count] {
			recipients = append(recipients, message.Recipient{UserID: contacts[index].ID})
		}
		createdAt := now.Add(-time.Duration(opts.Messages-i) * time.Hour)
		msg := &message.Message{
			Title:     fmt.Sprintf("[%s] Notification %03d", opts.Prefix, i+1),
			Body:      fmt.Sprintf("Seeded message %d. Generated by internal/seed.", i+1),
			Type:      types[i%len(types)],
			Level:     levels[i%len(levels)],
			Link:      "/messages",
			Audience:  message.AudienceUsers,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
		if err := repo.CreateMessage(ctx, msg, recipients, nil, now); err != nil {
			return fmt.Errorf("create seed message %d: %w", i+1, err)
		}
		result.MessagesCreated++
		result.RecipientsCreated += len(recipients)

		for j := range recipients {
			if rng.Intn(2) == 0 {
				if err := repo.MarkRead(ctx, recipients[j].UserID, msg.ID, now); err != nil {
					return fmt.Errorf("mark seed message read: %w", err)
				}
				result.RecipientsRead++
			}
		}
	}
	return nil
}

type dictItem struct {
	key       string
	label     string
	value     string
	valueType dict.ValueType
	sort      int
}

func titlePrefix(prefix string) string {
	if prefix == "" {
		return "Seed"
	}
	return strings.ToUpper(prefix[:1]) + prefix[1:]
}
