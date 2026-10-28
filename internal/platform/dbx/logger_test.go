package dbx

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/example/go-web-template/internal/platform/config"
)

// The suite below replaces the old "everything through Printf at info level"
// adapter. Those tests fail if a SQL error goes back to being an info line:
// level-filtered log pipelines (and the alert on error-level entries) would
// silently stop seeing database failures.

func TestGormLoggerLogsSQLFailureAtErrorLevel(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	db := openSqlite(t, &config.Database{LogLevel: "error", SlowThreshold: time.Second}, zap.New(core))

	if err := db.Exec("SELECT * FROM table_that_does_not_exist").Error; err == nil {
		t.Fatal("expected the query to fail")
	}

	entries := logs.FilterMessage("sql statement failed").All()
	if len(entries) != 1 {
		t.Fatalf("error entries = %d, want 1 (all: %v)", len(entries), logs.All())
	}
	if entries[0].Level != zapcore.ErrorLevel {
		t.Errorf("level = %v, want error", entries[0].Level)
	}
	fields := entries[0].ContextMap()
	if fields["sql"] == "" {
		t.Errorf("missing sql field: %v", fields)
	}
	if _, ok := fields["elapsed"]; !ok {
		t.Errorf("missing elapsed field: %v", fields)
	}
	if info := logs.FilterLevelExact(zapcore.InfoLevel).All(); len(info) != 0 {
		t.Errorf("SQL failure also logged at info level: %v", info)
	}
}

func TestGormLoggerSlowQueryLogsAtWarnLevel(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	// A nanosecond threshold makes every statement "slow" deterministically.
	db := openSqlite(t, &config.Database{LogLevel: "warn", SlowThreshold: time.Nanosecond}, zap.New(core))

	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("query: %v", err)
	}

	entries := logs.FilterMessage("slow sql").All()
	if len(entries) != 1 {
		t.Fatalf("warn entries = %d, want 1 (all: %v)", len(entries), logs.All())
	}
	if entries[0].Level != zapcore.WarnLevel {
		t.Errorf("level = %v, want warn", entries[0].Level)
	}
}

func TestGormLoggerInfoLogsSuccessfulQueries(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	db := openSqlite(t, &config.Database{LogLevel: "info", SlowThreshold: time.Hour}, zap.New(core))

	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("query: %v", err)
	}

	entries := logs.FilterMessage("sql").All()
	if len(entries) != 1 {
		t.Fatalf("info entries = %d, want 1 (all: %v)", len(entries), logs.All())
	}
	if entries[0].Level != zapcore.InfoLevel {
		t.Errorf("level = %v, want info", entries[0].Level)
	}
}

func TestGormLoggerSilentLogsNothing(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	db := openSqlite(t, &config.Database{LogLevel: "silent", SlowThreshold: time.Nanosecond}, zap.New(core))

	_ = db.Exec("SELECT * FROM table_that_does_not_exist").Error
	if entries := logs.All(); len(entries) != 0 {
		t.Fatalf("silent logger produced %d entries: %v", len(entries), entries)
	}

	// LogMode must work on the copy too (gorm uses it for per-session levels).
	silent := newGormLogger(&config.Database{LogLevel: "info", SlowThreshold: time.Hour}, zap.New(core)).
		LogMode(gormlogger.Silent)
	silent.Error(t.Context(), "should not be logged")
	if entries := logs.All(); len(entries) != 0 {
		t.Fatalf("LogMode(silent) produced %d entries: %v", len(entries), entries)
	}
}

func TestGormLoggerKeepsQueriesParameterized(t *testing.T) {
	logger := newGormLogger(&config.Database{LogLevel: "warn", SlowThreshold: time.Second}, zap.NewNop())

	filter, ok := logger.(gorm.ParamsFilter)
	if !ok {
		t.Fatal("logger must implement gormlogger.ParamsFilter, otherwise parameters are interpolated into logged SQL")
	}
	sql, params := filter.ParamsFilter(t.Context(), "SELECT * FROM users WHERE email = ?", "user@example.com")
	if sql != "SELECT * FROM users WHERE email = ?" {
		t.Errorf("sql = %q, want the placeholder form", sql)
	}
	if params != nil {
		t.Errorf("params = %v, want nil (parameterized logging)", params)
	}
}

func TestGormLoggerInfoAndWarnHelpers(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := newGormLogger(&config.Database{LogLevel: "info", SlowThreshold: time.Hour}, zap.New(core))

	logger.Info(t.Context(), "gorm info %d", 1)
	logger.Warn(t.Context(), "gorm warn")

	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (all: %v)", len(entries), entries)
	}
	if entries[0].Level != zapcore.InfoLevel || entries[0].Message != "gorm info 1" {
		t.Errorf("first entry = %v %q, want info \"gorm info 1\"", entries[0].Level, entries[0].Message)
	}
	if entries[1].Level != zapcore.WarnLevel || entries[1].Message != "gorm warn" {
		t.Errorf("second entry = %v %q, want warn \"gorm warn\"", entries[1].Level, entries[1].Message)
	}
}

// openSqlite wires an in-memory database with the production logger, so the
// tests exercise the real gorm call sites (Trace/Error) instead of calling the
// adapter directly.
func openSqlite(t *testing.T, cfg *config.Database, logger *zap.Logger) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:gorm-logger?mode=memory&cache=shared"), &gorm.Config{
		Logger: newGormLogger(cfg, logger),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
