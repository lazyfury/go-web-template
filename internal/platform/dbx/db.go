// Package dbx implements the data-access layer on top of GORM.
package dbx

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	gormlogger "gorm.io/gorm/logger"

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
		Logger: newGormLogger(cfg.LogLevel, logger),
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

// newGormLogger adapts zap to the gorm logger interface.
func newGormLogger(level string, logger *zap.Logger) gormlogger.Interface {
	var gormLevel gormlogger.LogLevel
	switch level {
	case "silent":
		gormLevel = gormlogger.Silent
	case "error":
		gormLevel = gormlogger.Error
	case "info":
		gormLevel = gormlogger.Info
	default:
		gormLevel = gormlogger.Warn
	}
	return gormlogger.New(
		&zapWriter{logger: logger},
		gormlogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormLevel,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		},
	)
}

// zapWriter adapts a *zap.Logger to gorm's logger.Writer interface.
type zapWriter struct {
	logger *zap.Logger
}

func (w *zapWriter) Printf(format string, args ...any) {
	w.logger.Info(fmt.Sprintf(format, args...))
}
