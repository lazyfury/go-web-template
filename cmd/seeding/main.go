// Command seeding creates deterministic demo data for local development.
//
// It is intentionally separate from cmd/server: it opens the database, writes
// fixtures through the domain repositories, and exits. It never starts HTTP or
// the message worker.
//
// Examples:
//
//	go run ./cmd/seeding --users 20 --messages 10
//	go run ./cmd/seeding users --users 5
//	go run ./cmd/seeding --only dict,templates
//
// The command refuses to run with app.env=production unless --allow-production
// is passed explicitly.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/cli"
	"github.com/example/go-web-template/internal/seed"
)

const defaultSeedPassword = "seed-password-123"

type seedingCommand struct {
	config          cli.ConfigOptions
	options         seed.Options
	only            []string
	allowProduction bool
}

func main() {
	ctx, stop := cli.SignalContext()

	state := &seedingCommand{}
	root := &cobra.Command{
		Use:           "seeding",
		Short:         "Create deterministic demo data for local development",
		Long:          "Create fake users, dictionaries, templates and inbox messages. Re-running is idempotent.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSeeding(ctx, state, state.only)
		},
	}
	cli.AddConfigFlags(root, &state.config)
	root.PersistentFlags().IntVarP(&state.options.Users, "users", "u", 20, "number of fake users to create")
	root.PersistentFlags().IntVarP(&state.options.Messages, "messages", "m", 10, "number of inbox messages to create")
	root.PersistentFlags().IntVar(&state.options.MaxRecipients, "max-recipients", 5, "max users receiving one seeded message")
	root.PersistentFlags().IntVar(&state.options.AdminEvery, "admin-every", 0, "promote every Nth seeded user to admin (0 = never)")
	root.PersistentFlags().StringVar(&state.options.Prefix, "prefix", "seed", "namespace for emails, messages, dict and template keys")
	root.PersistentFlags().StringVar(&state.options.Password, "password", "", "shared test password (prefer APP_SEED_PASSWORD)")
	root.PersistentFlags().IntVar(&state.options.BcryptCost, "bcrypt-cost", 4, "bcrypt cost for seeded users (test-only; 4-14)")
	root.PersistentFlags().Int64Var(&state.options.RandomSeed, "random-seed", 42, "deterministic seed for recipient selection")
	root.PersistentFlags().BoolVar(&state.allowProduction, "allow-production", false, "allow seeding when app.env=production (dangerous)")
	root.Flags().StringSliceVar(&state.only, "only", nil, "seed only these parts: users,dict,templates,messages")

	root.AddCommand(
		newPartCommand(ctx, state, "users", "Seed users only", []string{seed.PartUsers}),
		newPartCommand(ctx, state, "dicts", "Seed demo dictionaries only", []string{seed.PartDict}),
		newPartCommand(ctx, state, "templates", "Seed demo message templates only", []string{seed.PartTemplates}),
		newPartCommand(ctx, state, "messages", "Seed inbox messages for existing prefixed users", []string{seed.PartMessages}),
	)

	executeErr := root.ExecuteContext(ctx)
	stop()
	if executeErr != nil {
		log.Fatal(executeErr)
	}
}

func newPartCommand(ctx context.Context, state *seedingCommand, use, short string, only []string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSeeding(ctx, state, only)
		},
	}
}

func runSeeding(ctx context.Context, state *seedingCommand, only []string) error {
	opts := state.options
	opts.Only = only
	opts.Password = resolvePassword(opts.Password)

	cfg, err := cli.LoadConfig(state.config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.IsProduction() && !state.allowProduction {
		return fmt.Errorf("refusing to seed app.env=production; pass --allow-production only for a disposable environment")
	}

	logger, err := cli.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush

	if opts.AdminEvery > 0 {
		logger.Warn("seeding admin users with a known shared password", zap.Int("admin_every", opts.AdminEvery))
	}
	if opts.Password == defaultSeedPassword {
		logger.Warn("using the default seed password; set APP_SEED_PASSWORD for anything shared")
	}

	db, closeDB, err := cli.OpenDB(cfg, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() {
		if err := closeDB(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	result, err := seed.Run(ctx, db, &opts)
	if err != nil {
		return fmt.Errorf("seed fixtures: %w", err)
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
	return nil
}

func resolvePassword(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("APP_SEED_PASSWORD"); env != "" {
		return env
	}
	return defaultSeedPassword
}
