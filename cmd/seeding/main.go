// Command seeding creates deterministic demo data for local development.
//
// It is intentionally separate from cmd/server: it opens the database, writes
// fixtures through the domain repositories, and exits. It never starts the HTTP
// server or the message worker.
//
// Usage:
//
//	go run ./cmd/seeding -users 20 -messages 10
//	APP_SEED_PASSWORD='seed-password' go run ./cmd/seeding -prefix seed
//
// The command refuses to run with app.env=production unless -allow-production
// is passed explicitly.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/dbx"
	"github.com/example/go-web-template/internal/platform/logx"
	"github.com/example/go-web-template/internal/seed"
)

const defaultSeedPassword = "seed-password-123"

func main() {
	var (
		configFile    = flag.String("config", "", "path to config file (default: configs/config.yaml)")
		envPrefix     = flag.String("env-prefix", "APP", "environment variable prefix for config overrides")
		users         = flag.Int("users", 20, "number of fake users to create")
		messages      = flag.Int("messages", 10, "number of inbox messages to create")
		maxRecipients = flag.Int("max-recipients", 5, "max users receiving one seeded message")
		adminEvery    = flag.Int("admin-every", 0, "promote every Nth seeded user to admin (0 = never)")
		prefix        = flag.String("prefix", "seed", "namespace for emails, messages, dict and template keys")
		password      = flag.String("password", "", "shared test password (prefer APP_SEED_PASSWORD)")
		bcryptCost    = flag.Int("bcrypt-cost", 4, "bcrypt cost for seeded users (test-only; 4-14)")
		randomSeed    = flag.Int64("random-seed", 42, "deterministic seed for recipient selection")
		allowProd     = flag.Bool("allow-production", false, "allow seeding when app.env=production (dangerous)")
	)
	flag.Parse()

	passwordValue := *password
	if passwordValue == "" {
		passwordValue = os.Getenv("APP_SEED_PASSWORD")
	}
	if passwordValue == "" {
		passwordValue = defaultSeedPassword
	}

	cfg, err := config.Load(*configFile, *envPrefix)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if cfg.IsProduction() && !*allowProd {
		log.Fatalf("refusing to seed app.env=production; pass -allow-production only for a disposable environment")
	}

	logger, err := logx.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush

	if *adminEvery > 0 {
		logger.Warn("seeding admin users with a known shared password",
			zap.Int("admin_every", *adminEvery),
		)
	}
	if passwordValue == defaultSeedPassword {
		logger.Warn("using the default seed password; set APP_SEED_PASSWORD for anything shared")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := dbx.New(&cfg.Database, logger)
	if err != nil {
		logger.Fatal("open database", zap.Error(err))
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.Fatal("access sql.DB", zap.Error(err))
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	result, err := seed.Run(ctx, db, &seed.Options{
		Users:         *users,
		Messages:      *messages,
		MaxRecipients: *maxRecipients,
		AdminEvery:    *adminEvery,
		Prefix:        *prefix,
		Password:      passwordValue,
		BcryptCost:    *bcryptCost,
		RandomSeed:    *randomSeed,
	})
	if err != nil {
		logger.Fatal("seed fixtures", zap.Error(err))
	}

	logger.Info("seeding finished",
		zap.Int("users_created", result.UsersCreated),
		zap.Int("users_skipped", result.UsersSkipped),
		zap.Int("dict_groups_created", result.DictGroupsCreated),
		zap.Int("dict_groups_skipped", result.DictGroupsSkipped),
		zap.Int("dict_items_created", result.DictItemsCreated),
		zap.Int("dict_items_skipped", result.DictItemsSkipped),
		zap.Int("templates_created", result.TemplatesCreated),
		zap.Int("templates_skipped", result.TemplatesSkipped),
		zap.Int("messages_created", result.MessagesCreated),
		zap.Int("messages_skipped", result.MessagesSkipped),
		zap.Int("recipients_created", result.RecipientsCreated),
		zap.Int("recipients_read", result.RecipientsRead),
	)
}
