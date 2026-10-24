// Command admin provisions or resets an administrator account.
//
// The template has no public signup: a fresh deployment is bootstrapped by
// running this binary once (or by `make seed-admin`), then logging in through
// POST /api/v1/auth/login.
//
// Examples:
//
//	APP_SEED_PASSWORD='...' go run ./cmd/admin seed --email admin@example.com
//	go run ./cmd/admin show --email admin@example.com
//
// The password is read from APP_SEED_PASSWORD (or --password) and is never
// echoed to stdout or the logs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/cli"
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/passwd"
	"github.com/example/go-web-template/internal/user"
)

const minSeedPassword = 8

type adminCommand struct {
	config   cli.ConfigOptions
	email    string
	name     string
	role     string
	password string
}

func main() {
	ctx, stop := cli.SignalContext()

	state := &adminCommand{}
	root := &cobra.Command{
		Use:           "admin",
		Short:         "Bootstrap or inspect administrator accounts",
		Long:          "Create/reset an administrator account, or inspect an existing account. There is no public signup.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSeed(ctx, state)
		},
	}
	cli.AddConfigFlags(root, &state.config)
	root.PersistentFlags().StringVar(&state.email, "email", "admin@example.com", "account email")
	root.PersistentFlags().StringVar(&state.name, "name", "Administrator", "display name")
	root.PersistentFlags().StringVar(&state.role, "role", "admin", "role to grant (admin|member)")
	root.PersistentFlags().StringVar(&state.password, "password", "", "password (prefer APP_SEED_PASSWORD)")

	root.AddCommand(
		&cobra.Command{
			Use:   "seed",
			Short: "Create or reset the account (default)",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				return runSeed(ctx, state)
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Show an account without changing it",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				return runShow(ctx, state)
			},
		},
	)

	executeErr := root.ExecuteContext(ctx)
	stop()
	if executeErr != nil {
		log.Fatal(executeErr)
	}
}

func runSeed(ctx context.Context, state *adminCommand) error {
	password := state.password
	if password == "" {
		password = os.Getenv("APP_SEED_PASSWORD")
	}
	if len(password) < minSeedPassword || len(password) > passwd.MaxPasswordBytes {
		return fmt.Errorf("password must be between %d and %d bytes; set APP_SEED_PASSWORD", minSeedPassword, passwd.MaxPasswordBytes)
	}
	role := authkit.Role(strings.TrimSpace(state.role))
	if !role.Valid() {
		return fmt.Errorf("invalid role %q (want admin or member)", state.role)
	}

	cfg, err := cli.LoadConfig(state.config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger, err := cli.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush

	db, closeDB, err := cli.OpenDB(cfg, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() {
		if err := closeDB(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	account, err := app.SeedAdmin(cfg, logger, db, app.SeedOptions{
		Name:     state.name,
		Email:    state.email,
		Password: password,
		Role:     role,
	})
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	fmt.Printf("seeded %s account: id=%d email=%s\n", account.Role, account.ID, account.Email)
	return nil
}

func runShow(ctx context.Context, state *adminCommand) error {
	cfg, err := cli.LoadConfig(state.config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger, err := cli.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush

	db, closeDB, err := cli.OpenDB(cfg, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() {
		if err := closeDB(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	account, err := user.NewRepository(db).FindByEmail(ctx, strings.TrimSpace(state.email))
	if errors.Is(err, apperr.ErrNotFound) {
		return fmt.Errorf("account not found: %s", state.email)
	}
	if err != nil {
		return fmt.Errorf("find account: %w", err)
	}
	fmt.Printf("id=%d name=%s email=%s role=%s status=%d\n",
		account.ID, account.Name, account.Email, account.Role, account.Status)
	return nil
}
