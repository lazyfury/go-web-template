package seed

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/dict"
	"github.com/example/go-web-template/internal/message"
	"github.com/example/go-web-template/internal/testutil"
	"github.com/example/go-web-template/internal/user"
)

func newSeedDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewSQLiteDB(t,
		&user.User{},
		&dict.Group{}, &dict.Item{},
		&message.Message{}, &message.Recipient{}, &message.Delivery{}, &message.Template{},
	)
}

func TestRunCreatesAndSkipsFixtures(t *testing.T) {
	db := newSeedDB(t)
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	opts := Options{
		Users:         3,
		Messages:      2,
		MaxRecipients: 2,
		AdminEvery:    2,
		Prefix:        "seed",
		Password:      "seed-password",
		BcryptCost:    4,
		RandomSeed:    7,
		Now:           func() time.Time { return now },
	}

	result, err := Run(context.Background(), db, &opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.UsersCreated != 3 || result.UsersSkipped != 0 {
		t.Errorf("users = %+v", result)
	}
	if result.DictGroupsCreated != 3 || result.DictItemsCreated != 10 {
		t.Errorf("dicts = %+v", result)
	}
	if result.TemplatesCreated != 4 {
		t.Errorf("templates = %+v", result)
	}
	if result.MessagesCreated != 2 || result.RecipientsCreated == 0 {
		t.Errorf("messages = %+v", result)
	}

	var userCount, groupCount, itemCount, templateCount, messageCount, recipientCount int64
	db.Model(&user.User{}).Count(&userCount)
	db.Model(&dict.Group{}).Count(&groupCount)
	db.Model(&dict.Item{}).Count(&itemCount)
	db.Model(&message.Template{}).Count(&templateCount)
	db.Model(&message.Message{}).Count(&messageCount)
	db.Model(&message.Recipient{}).Count(&recipientCount)
	if userCount != 3 || groupCount != 3 || itemCount != 10 || templateCount != 4 || messageCount != 2 {
		t.Errorf("counts: users=%d groups=%d items=%d templates=%d messages=%d",
			userCount, groupCount, itemCount, templateCount, messageCount)
	}
	if recipientCount != int64(result.RecipientsCreated) {
		t.Errorf("recipient count = %d, want %d", recipientCount, result.RecipientsCreated)
	}

	var admins int64
	db.Model(&user.User{}).Where("role = ?", "admin").Count(&admins)
	if admins != 1 {
		t.Errorf("admin count = %d, want 1 (admin-every=2)", admins)
	}

	// Re-running must not duplicate any fixture.
	second, err := Run(context.Background(), db, &opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.UsersCreated != 0 || second.UsersSkipped != 3 {
		t.Errorf("second users = %+v", second)
	}
	if second.DictGroupsCreated != 0 || second.DictGroupsSkipped != 3 {
		t.Errorf("second groups = %+v", second)
	}
	if second.DictItemsCreated != 0 || second.DictItemsSkipped != 10 {
		t.Errorf("second items = %+v", second)
	}
	if second.TemplatesCreated != 0 || second.TemplatesSkipped != 4 {
		t.Errorf("second templates = %+v", second)
	}
	if second.MessagesCreated != 0 || second.MessagesSkipped != 2 {
		t.Errorf("second messages = %+v", second)
	}
	var after int64
	db.Model(&message.Message{}).Count(&after)
	if after != 2 {
		t.Errorf("messages after re-run = %d, want 2", after)
	}
}

func TestRunValidation(t *testing.T) {
	db := newSeedDB(t)
	valid := Options{Users: 1, Password: "seed-password", BcryptCost: 4}
	cases := []struct {
		name string
		opts Options
	}{
		{"bad prefix", Options{Users: 1, Password: "seed-password", Prefix: "Bad Prefix"}},
		{"short password", Options{Users: 1, Password: "short"}},
		{"negative users", Options{Users: -1, Password: "seed-password"}},
		{"bad bcrypt", Options{Users: 1, Password: "seed-password", BcryptCost: 2}},
	}
	for _, tc := range cases {
		if _, err := Run(context.Background(), db, &tc.opts); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}
	if _, err := Run(context.Background(), db, &valid); err != nil {
		t.Fatalf("valid options: %v", err)
	}
}
