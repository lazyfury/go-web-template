// Package testutil provides shared helpers for tests across packages.
// It is only imported from _test.go files, so the sqlite driver it pulls in
// never lands in the production binary.
package testutil

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/example/go-web-template/internal/model"
)

// Models lists every GORM model so tests can AutoMigrate the schema.
var Models = []any{
	&model.User{},
}

// NewSQLiteDB opens an in-memory SQLite database with the full schema
// migrated. Useful for handler/repository tests that need a real DB without
// spinning up PostgreSQL.
//
// Each caller gets its own named in-memory database (DSN derived from the
// test name) so tests can run in parallel and never share rows.
func NewSQLiteDB(tb testing.TB) *gorm.DB {
	tb.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", sanitizeDSN(tb.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		tb.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatalf("sqlite pool: %v", err)
	}
	// 内存库在最后一个连接关闭时销毁：测试结束时显式释放，避免跨测试泄漏。
	tb.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(Models...); err != nil {
		tb.Fatalf("migrate schema: %v", err)
	}
	return db
}

// sanitizeDSN keeps only characters that are safe inside a SQLite URI.
func sanitizeDSN(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
