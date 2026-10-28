// Command server is the HTTP API process of the go-web-template monolith.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/example/go-web-template/internal/app"
	"github.com/example/go-web-template/internal/platform/buildinfo"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/logx"
	"github.com/example/go-web-template/internal/platform/otelx"
)

// Build-time variables, injected via -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

// otelServiceName keeps the resource attribute in one place: an explicit
// setting wins, otherwise the app name (so a renamed template stays coherent).
func otelServiceName(cfg *config.Config) string {
	if cfg.OTel.ServiceName != "" {
		return cfg.OTel.ServiceName
	}
	return cfg.App.Name
}

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

	// Tracing is opt-in (otel.enabled): an unreachable collector must not turn
	// into a startup failure. Shutdown uses a fresh context because ctx is
	// already canceled when the deferred cleanup runs.
	tracing, err := otelx.New(ctx, otelx.Options{
		Enabled:     cfg.OTel.Enabled,
		ServiceName: otelServiceName(cfg),
		Version:     version,
		Endpoint:    cfg.OTel.Endpoint,
		Insecure:    cfg.OTel.Insecure,
		SampleRatio: cfg.OTel.SampleRatio,
	}, logger)
	if err != nil {
		logger.Fatal("init tracing", zap.Error(err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracing.Shutdown(shutdownCtx); err != nil {
			logger.Warn("shutdown tracing", zap.Error(err))
		}
	}()

	// build_info makes "which build is this alert/dashboard line about?"
	// answerable without grepping container logs.
	if err := buildinfo.Register(prometheus.DefaultRegisterer, version, commit); err != nil {
		logger.Warn("register build_info metric", zap.Error(err))
	}

	application, err := app.New(cfg, logger, app.WithVersion(version))
	if err != nil {
		logger.Fatal("init application", zap.Error(err))
	}
	defer func() {
		if err := application.Close(); err != nil {
			logger.Warn("close application", zap.Error(err))
		}
	}()

	// Log schema state once, so "which version is actually deployed" is in the
	// first lines of every container log (and not only behind /readyz).
	if status, err := application.SchemaStatus(ctx); err != nil {
		logger.Warn("inspect database schema", zap.Error(err))
	} else {
		logger.Info("database schema",
			zap.Int64("applied_version", status.Applied),
			zap.Int64("required_version", status.Required),
			zap.Bool("dirty", status.Dirty),
			zap.Bool("migrations_table", status.Found),
			zap.Bool("ready", status.OK()),
		)
		if !status.OK() {
			logger.Warn("database schema is not ready; /readyz will report 503", zap.String("reason", status.Reason()))
		}
	}

	// Root goroutines live here: HTTP, the message worker and the rate-limiter
	// sweeper share one lifecycle. A failure in any of them cancels the others;
	// App itself never starts goroutines.
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return application.Run(groupCtx) })
	if worker := application.MessageWorker(); worker != nil {
		group.Go(func() error { return worker.Run(groupCtx) })
	}
	if keeper := application.Housekeeper(); keeper != nil {
		group.Go(func() error { return keeper.Run(groupCtx) })
	}
	if limiters := application.Limiters(); limiters != nil && limiters.Len() > 0 {
		group.Go(func() error { return limiters.Run(groupCtx) })
	}
	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal("application exited with error", zap.Error(err))
	}
	logger.Info("application stopped")
}
