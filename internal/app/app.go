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
	"github.com/example/go-web-template/internal/platform/routekit"
	"github.com/example/go-web-template/internal/user"
	"github.com/example/go-web-template/internal/web"
)

// Options carries process-level metadata used by the composition root.
type Options struct {
	// Version is exposed in the OpenAPI document. Defaults to "dev".
	Version string
}

// Option customizes Options.
type Option func(*Options)

// WithVersion sets the version shown in the OpenAPI document.
func WithVersion(version string) Option {
	return func(options *Options) {
		options.Version = version
	}
}

// App owns the assembled HTTP stack and the database handle.
type App struct {
	db      *gorm.DB
	engine  *gin.Engine
	server  *web.Server
	openapi *routekit.Registry
}

// New opens the database and wires the application. It is the entry point used
// by cmd/server/main.go.
func New(cfg *config.Config, logger *zap.Logger, opts ...Option) (*App, error) {
	db, err := dbx.New(&cfg.Database, logger)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}
	return NewWithDB(cfg, logger, db, opts...)
}

// NewWithDB wires every component against an existing *gorm.DB. It exists so
// tests (and alternative drivers) can exercise the exact production wiring
// without opening a PostgreSQL connection.
func NewWithDB(cfg *config.Config, logger *zap.Logger, db *gorm.DB, opts ...Option) (*App, error) {
	options := Options{Version: "dev"}
	for _, opt := range opts {
		opt(&options)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access sql.DB: %w", err)
	}

	// Data layer → use-cases → HTTP adapter. Each domain is one package.
	userRepo := user.NewRepository(db)
	userService := user.NewService(userRepo)
	userHandler := user.NewHandler(userService)

	// Transport layer. web knows nothing about domains; the composition root
	// mounts each module on the versioned API group and then builds the
	// OpenAPI document from the registered metadata.
	engine, api := web.New(cfg, logger, web.NewHealth(sqlDB), options.Version)
	user.RegisterRoutes(api, userHandler)
	if err := web.MountDocs(engine, api, cfg.Swagger.Enabled); err != nil {
		return nil, fmt.Errorf("build openapi: %w", err)
	}

	return &App{
		db:      db,
		engine:  engine,
		server:  web.NewServer(fmt.Sprintf(":%d", cfg.App.Port), cfg.HTTPServer, engine, logger),
		openapi: api.Registry(),
	}, nil
}

// Handler exposes the HTTP handler (tests use it with httptest).
func (a *App) Handler() http.Handler { return a.engine }

// OpenAPI exposes the registry backing the generated document. Tests use it
// to assert that every gin route is documented.
func (a *App) OpenAPI() *routekit.Registry { return a.openapi }

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
