// Package app assembles all components and owns the application lifecycle.
// cmd/server/main.go stays a thin wrapper around app.Run.
package app

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/config"
	"github.com/example/go-web-template/internal/handler"
	"github.com/example/go-web-template/internal/repository"
	"github.com/example/go-web-template/internal/router"
	"github.com/example/go-web-template/internal/server"
	"github.com/example/go-web-template/internal/service"
)

// Run builds every component and blocks until ctx is canceled or the HTTP
// server fails. It is the single entry point used by main.go and smoke tests.
func Run(ctx context.Context, cfg *config.Config, logger *zap.Logger) error {
	// --- Data layer ------------------------------------------------------
	db, err := repository.NewDB(&cfg.Database, logger)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	userRepo := repository.NewUserRepository(db)

	// --- Service layer ---------------------------------------------------
	userService := service.NewUserService(userRepo)

	// --- HTTP layer ------------------------------------------------------
	healthHandler := handler.NewHealthHandler(db)
	userHandler := handler.NewUserHandler(userService)

	engine := router.New(cfg, logger, router.Handlers{
		User:   userHandler,
		Health: healthHandler,
	})

	srv := server.New(cfg, engine, logger)
	return srv.Run(ctx)
}
