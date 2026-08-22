// Package testutil provides shared helpers for tests across packages.
// It is only imported from _test.go files, so the sqlite driver it pulls in
// never lands in the production binary.
package testutil

import (
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
func NewSQLiteDB(tb testing.TB) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		tb.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(Models...); err != nil {
		tb.Fatalf("migrate schema: %v", err)
	}
	return db
}
