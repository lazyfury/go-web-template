package dbx

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/testutil"
)

// TestRequiredSchemaVersionMatchesMigrations keeps the constant honest: adding
// a migration without bumping it would make /readyz accept a database that is
// missing the new table/column, and bumping it without a migration would make
// every deployment report unready.
func TestRequiredSchemaVersionMatchesMigrations(t *testing.T) {
	entries, err := filepath.Glob("../../../migrations/*.up.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migration files found")
	}
	var maxVersion int64
	for _, entry := range entries {
		name := filepath.Base(entry)
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Fatalf("unexpected migration name %q", name)
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("parse version of %q: %v", name, err)
		}
		if version > maxVersion {
			maxVersion = version
		}
	}
	if RequiredSchemaVersion != maxVersion {
		t.Fatalf("RequiredSchemaVersion = %d, newest migration = %d (bump the constant with the migration)", RequiredSchemaVersion, maxVersion)
	}

	// Every up must have a down, otherwise a rollback silently leaves the
	// schema half-applied.
	for _, entry := range entries {
		down := strings.Replace(entry, ".up.sql", ".down.sql", 1)
		if _, err := os.Stat(down); err != nil {
			t.Errorf("missing down migration for %s", filepath.Base(entry))
		}
	}
}

func newSchemaDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewSQLiteDB(t, &schemaUsersProbe{})
}

// schemaUsersProbe creates a "users" table so HasTable("users") is true; dbx
// must not import any domain package just to probe the schema.
type schemaUsersProbe struct {
	ID uint64 `gorm:"primaryKey"`
}

func (schemaUsersProbe) TableName() string { return "users" }

func TestInspectSchemaWithoutMigrations(t *testing.T) {
	db := newSchemaDB(t)

	status, err := InspectSchema(context.Background(), db)
	if err != nil {
		t.Fatalf("InspectSchema: %v", err)
	}
	if status.Found {
		t.Error("schema_migrations should not exist")
	}
	if !status.CoreTables {
		t.Error("users table should exist")
	}
	if !status.OK() {
		t.Errorf("a database created by AutoMigrate must stay ready: %+v reason=%q", status, status.Reason())
	}
	if status.Required != RequiredSchemaVersion {
		t.Errorf("required = %d, want %d", status.Required, RequiredSchemaVersion)
	}
}

func TestInspectSchemaWithoutAnyTables(t *testing.T) {
	db := testutil.NewSQLiteDB(t)

	status, err := InspectSchema(context.Background(), db)
	if err != nil {
		t.Fatalf("InspectSchema: %v", err)
	}
	if status.OK() {
		t.Errorf("an empty database must not be ready: %+v", status)
	}
	if got := status.Reason(); !strings.Contains(got, "not applied") {
		t.Errorf("reason = %q, want 'not applied'", got)
	}
}

func TestInspectSchemaVersionAndDirty(t *testing.T) {
	tests := []struct {
		name       string
		version    int64
		dirty      bool
		wantOK     bool
		wantReason string
	}{
		{name: "current", version: RequiredSchemaVersion, wantOK: true},
		{name: "newer than binary", version: RequiredSchemaVersion + 1, wantOK: true},
		{name: "behind", version: RequiredSchemaVersion - 1, wantReason: "schema behind"},
		{name: "dirty", version: RequiredSchemaVersion, dirty: true, wantReason: "dirty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newSchemaDB(t)
			mustExec(t, db, "CREATE TABLE schema_migrations (version bigint, dirty boolean)")
			mustExec(t, db, "INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)", tt.version, tt.dirty)

			status, err := InspectSchema(context.Background(), db)
			if err != nil {
				t.Fatalf("InspectSchema: %v", err)
			}
			if !status.Found || status.Applied != tt.version || status.Dirty != tt.dirty {
				t.Fatalf("status = %+v, want version=%d dirty=%v", status, tt.version, tt.dirty)
			}
			if status.OK() != tt.wantOK {
				t.Errorf("OK() = %v, want %v (reason=%q)", status.OK(), tt.wantOK, status.Reason())
			}
			if tt.wantReason != "" && !strings.Contains(status.Reason(), tt.wantReason) {
				t.Errorf("reason = %q, want %q", status.Reason(), tt.wantReason)
			}
		})
	}
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
