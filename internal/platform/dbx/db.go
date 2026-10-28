// Package dbx implements the data-access layer on top of GORM.
package dbx

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/config"
)

// New opens a PostgreSQL connection pool with sane production defaults.
func New(cfg *config.Database, logger *zap.Logger) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newGormLogger(cfg, logger),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	if cfg.ConnMaxLifetime <= 0 {
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// Fail fast: verify the connection is actually usable.
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
