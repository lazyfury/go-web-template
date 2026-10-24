// Package cli contains the shared bootstrap helpers for the repository's
// command-line tools: config/logging/database setup and signal handling.
package cli

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/dbx"
	"github.com/example/go-web-template/internal/platform/logx"
)

// ConfigOptions holds the common config-file flags shared by cmd/admin and
// cmd/seeding.
type ConfigOptions struct {
	File      string
	EnvPrefix string
}

// AddConfigFlags binds --config and --env-prefix as persistent flags.
func AddConfigFlags(cmd *cobra.Command, opts *ConfigOptions) {
	cmd.PersistentFlags().StringVar(&opts.File, "config", "", "path to config file (default: configs/config.yaml)")
	cmd.PersistentFlags().StringVar(&opts.EnvPrefix, "env-prefix", "APP", "environment variable prefix for config overrides")
}

// LoadConfig loads the application configuration.
func LoadConfig(opts ConfigOptions) (*config.Config, error) {
	return config.Load(opts.File, opts.EnvPrefix)
}

// NewLogger builds the process logger from config.
func NewLogger(cfg *config.Config) (*zap.Logger, error) {
	return logx.New(cfg.Log.Level, cfg.Log.Format)
}

// OpenDB opens the configured PostgreSQL pool and returns a close function.
func OpenDB(cfg *config.Config, logger *zap.Logger) (*gorm.DB, func() error, error) {
	db, err := dbx.New(&cfg.Database, logger)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	return db, sqlDB.Close, nil
}

// SignalContext returns a context canceled by SIGINT/SIGTERM. CLI commands are
// short-lived, but this keeps Ctrl-C from waiting on a slow DB/HTTP call.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
