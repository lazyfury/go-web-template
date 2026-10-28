package dbx

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/testutil"
)

// TestRegisterPoolMetrics pins both the metric set and the idempotency that
// keeps a test binary building several Apps in one process from panicking.
func TestRegisterPoolMetrics(t *testing.T) {
	gormDB := testutil.NewSQLiteDB(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}

	reg := prometheus.NewRegistry()
	if err := RegisterPoolMetrics(reg, sqlDB); err != nil {
		t.Fatalf("RegisterPoolMetrics: %v", err)
	}
	if err := RegisterPoolMetrics(reg, sqlDB); err != nil {
		t.Fatalf("second RegisterPoolMetrics must be a no-op: %v", err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	seen := make(map[string]bool, len(families))
	for _, family := range families {
		seen[family.GetName()] = true
	}
	for _, name := range []string{
		"db_pool_open_connections",
		"db_pool_in_use_connections",
		"db_pool_idle_connections",
		"db_pool_max_open_connections",
		"db_pool_wait_count_total",
		"db_pool_wait_duration_seconds_total",
		"db_pool_max_idle_closed_total",
		"db_pool_max_lifetime_closed_total",
	} {
		if !seen[name] {
			t.Errorf("metric %q is not registered", name)
		}
	}

	if err := RegisterPoolMetrics(prometheus.NewRegistry(), nil); err != nil {
		t.Errorf("nil pool must be a no-op: %v", err)
	}
}

// TestNewFailsFastOnUnreachableDatabase pins the startup contract: a bad
// database must abort construction (cmd/server then exits non-zero) instead of
// booting an app that answers 500 on every request.
func TestNewFailsFastOnUnreachableDatabase(t *testing.T) {
	cfg := &config.Database{
		Host:          "127.0.0.1",
		Port:          1, // reserved, nothing listens here
		User:          "nobody",
		Name:          "nobody",
		SSLMode:       "disable",
		LogLevel:      "silent",
		SlowThreshold: time.Second,
	}
	if _, err := New(cfg, zap.NewNop()); err == nil {
		t.Fatal("New() must fail when the database is unreachable")
	}
}
