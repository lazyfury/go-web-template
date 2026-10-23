// Package app is the composition root: it wires concrete implementations
// (database, repositories, services, handlers, router) and owns the process
// lifecycle. cmd/server/main.go stays a thin wrapper around it.
package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/dbx"
	"github.com/example/go-web-template/internal/user"
	"github.com/example/go-web-template/internal/web"
)

// App owns the assembled HTTP stack and the database handle.
type App struct {
	db     *gorm.DB
	engine *gin.Engine
	server *web.Server
}

// New opens the database and wires the application. It is the entry point used
// by cmd/server/main.go.
func New(cfg *config.Config, logger *zap.Logger) (*App, error) {
	db, err := dbx.New(&cfg.Database, logger)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}
	return NewWithDB(cfg, logger, db)
}

// NewWithDB wires every component against an existing *gorm.DB. It exists so
// tests (and alternative drivers) can exercise the exact production wiring
// without opening a PostgreSQL connection.
func NewWithDB(cfg *config.Config, logger *zap.Logger, db *gorm.DB) (*App, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access sql.DB: %w", err)
	}

	// Data layer → use-cases → HTTP adapter. Each domain is one package.
	userRepo := user.NewRepository(db)
	userService := user.NewService(userRepo)
	userHandler := user.NewHandler(userService)

	// Transport layer. web knows nothing about domains; the composition root
	// mounts the module on the versioned API group.
	engine, v1 := web.New(cfg, logger, web.NewHealth(sqlDB))
	user.RegisterRoutes(v1, userHandler)

	return &App{
		db:     db,
		engine: engine,
		server: web.NewServer(fmt.Sprintf(":%d", cfg.App.Port), cfg.HTTPServer, engine, logger),
	}, nil
}

// Handler exposes the HTTP handler (tests use it with httptest).
func (a *App) Handler() http.Handler { return a.engine }

// Run blocks until ctx is canceled, then drains connections gracefully.
func (a *App) Run(ctx context.Context) error { return a.server.Run(ctx) }

// Close releases the database connection pool.
func (a *App) Close() error {
	sqlDB, err := a.db.DB()
	if err != nil {
		return fmt.Errorf("access sql.DB: %w", err)
	}
	return sqlDB.Close()
}
