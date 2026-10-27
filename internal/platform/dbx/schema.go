package dbx

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RequiredSchemaVersion is the last golang-migrate version this binary was
// written against. Bump it together with every new migration pair:
// TestRequiredSchemaVersionMatchesMigrations reads ../../../migrations and
// fails when the constant and the files drift apart.
const RequiredSchemaVersion int64 = 7

// SchemaStatus is what /readyz needs to know about the database schema.
type SchemaStatus struct {
	// Applied is the version recorded in schema_migrations (0 when unknown).
	Applied int64
	// Required is the version this binary expects.
	Required int64
	// Dirty means golang-migrate failed mid-migration and needs manual repair.
	Dirty bool
	// Found reports whether the schema_migrations table exists at all. A
	// database migrated by something else (for example AutoMigrate in tests)
	// has the tables but no version row.
	Found bool
	// CoreTables reports whether the application tables exist. It separates
	// "never migrated" from "migrated by another tool" without guessing.
	CoreTables bool
}

// OK reports whether the schema is safe to serve traffic from.
//
// A missing migration table is only a failure when the core tables are missing
// too: tests and dev databases created by AutoMigrate have no version row, and
// reporting them unready would make the probe useless for every local setup.
// Deployments that use golang-migrate always have the table, so a real
// "forgot to migrate" (or "half migrated") database is caught.
func (s SchemaStatus) OK() bool {
	if s.Dirty {
		return false
	}
	if !s.Found {
		return s.CoreTables
	}
	return s.Applied >= s.Required
}

// Reason returns a short, log/JSON friendly explanation for a non-OK status.
func (s SchemaStatus) Reason() string {
	switch {
	case s.Dirty:
		return "schema migration dirty (manual repair required)"
	case !s.Found && !s.CoreTables:
		return "schema not applied (run migrations)"
	case s.Found && s.Applied < s.Required:
		return fmt.Sprintf("schema behind: applied %d, required %d", s.Applied, s.Required)
	default:
		return ""
	}
}

// InspectSchema reads the golang-migrate bookkeeping table and checks that the
// application tables exist. It is portable SQL (the tests run on SQLite).
func InspectSchema(ctx context.Context, db *gorm.DB) (SchemaStatus, error) {
	status := SchemaStatus{Required: RequiredSchemaVersion}

	migrator := db.WithContext(ctx).Migrator()
	status.CoreTables = migrator.HasTable("users")
	status.Found = migrator.HasTable("schema_migrations")
	if !status.Found {
		return status, nil
	}

	var row struct {
		Version int64
		Dirty   bool
	}
	err := db.WithContext(ctx).
		Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").
		Scan(&row).Error
	if err != nil {
		return status, fmt.Errorf("read schema_migrations: %w", err)
	}
	status.Applied = row.Version
	status.Dirty = row.Dirty
	return status, nil
}
