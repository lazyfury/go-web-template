// Command server is the HTTP API process of the go-web-template monolith.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/logx"
)

// Build-time variables, injected via -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	var (
		configFile = flag.String("config", "", "path to config file (default: configs/config.yaml)")
		envPrefix  = flag.String("env-prefix", "APP", "environment variable prefix for config overrides")
	)
	flag.Parse()

	cfg, err := config.Load(*configFile, *envPrefix)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger, err := logx.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush on exit
	logger.Info("starting application",
		zap.String("name", cfg.App.Name),
		zap.String("env", cfg.App.Env),
		zap.String("version", version),
		zap.String("commit", commit),
	)

	// Signal-aware context: SIGINT / SIGTERM trigger graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.New(cfg, logger, app.WithVersion(version))
	if err != nil {
		logger.Fatal("init application", zap.Error(err))
	}
	defer func() {
		if err := application.Close(); err != nil {
			logger.Warn("close application", zap.Error(err))
		}
	}()

	if err := application.Run(ctx); err != nil {
		logger.Fatal("application exited with error", zap.Error(err))
	}
	logger.Info("application stopped")
}
