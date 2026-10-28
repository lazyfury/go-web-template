package dbx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	gormlogger "gorm.io/gorm/logger"

	"github.com/example/go-web-template/internal/platform/config"
)

// gormLogger adapts *zap.Logger to gorm's logger.Interface with honest levels.
//
// Why not gormlogger.New + a Writer: that combination funnels info, slow-query
// and SQL-error messages through one Printf, so the writer has no level to
// attach and every line lands on INFO. A failed statement then disappears from
// level-based filtering and alerting (the access log has the 500, but the SQL
// cause is filed under info). Implementing the interface keeps each branch on
// its own zap level and produces structured fields instead of a preformatted
// blob.
type gormLogger struct {
	level          gormlogger.LogLevel
	slowThreshold  time.Duration
	ignoreNotFound bool
	// parameterized mirrors gorm's ParameterizedQueries: the SQL is logged with
	// placeholders instead of interpolated values, so no user data (emails,
	// tokens hashed into queries, ...) ends up in the logs.
	parameterized bool
	logger        *zap.Logger
}

func newGormLogger(cfg *config.Database, logger *zap.Logger) gormlogger.Interface {
	return &gormLogger{
		level:          gormLogLevel(cfg.LogLevel),
		slowThreshold:  cfg.SlowThreshold,
		ignoreNotFound: true,
		parameterized:  true,
		logger:         logger,
	}
}

// gormLogLevel maps the textual setting to gorm's level. Unknown values fall
// back to warn, matching the previous behavior.
func gormLogLevel(text string) gormlogger.LogLevel {
	switch text {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

// LogMode returns a copy with a different level, like gorm's own logger.
func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

// Info logs gorm's informational messages (migrator/tx bookkeeping).
func (l *gormLogger) Info(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Info {
		l.logger.Info(formatGormMessage(msg, data...))
	}
}

// Warn logs gorm warnings.
func (l *gormLogger) Warn(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Warn {
		l.logger.Warn(formatGormMessage(msg, data...))
	}
}

// Error logs gorm errors.
func (l *gormLogger) Error(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Error {
		l.logger.Error(formatGormMessage(msg, data...))
	}
}

// Trace logs one statement: failures at error level, slow statements at warn,
// everything else (only at info level) as info.
func (l *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	switch {
	case err != nil && l.level >= gormlogger.Error && (!errors.Is(err, gormlogger.ErrRecordNotFound) || !l.ignoreNotFound):
		sql, rows := fc()
		l.logger.Error("sql statement failed",
			zap.String("sql", sql),
			zap.Int64("rows", rows),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)
	case l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		l.logger.Warn("slow sql",
			zap.String("sql", sql),
			zap.Int64("rows", rows),
			zap.Duration("elapsed", elapsed),
			zap.Duration("slow_threshold", l.slowThreshold),
		)
	case l.level >= gormlogger.Info:
		sql, rows := fc()
		l.logger.Info("sql",
			zap.String("sql", sql),
			zap.Int64("rows", rows),
			zap.Duration("elapsed", elapsed),
		)
	}
}

// ParamsFilter keeps gorm's ParameterizedQueries behavior: returning nil
// parameters makes gorm log the SQL with placeholders instead of values.
// gorm detects this optional interface with a type assertion.
func (l *gormLogger) ParamsFilter(_ context.Context, sql string, params ...any) (filteredSQL string, filteredParams []any) {
	if l.parameterized {
		return sql, nil
	}
	return sql, params
}

// formatGormMessage keeps vet happy for the zero-argument case (gorm sometimes
// passes a plain message) while still supporting its printf-style calls.
func formatGormMessage(msg string, data ...any) string {
	if len(data) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, data...)
}
