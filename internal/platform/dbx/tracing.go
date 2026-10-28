package dbx

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// tracerName prefixes every DB span; the trace UI groups them under the service.
const tracerName = "github.com/example/go-web-template/dbx"

// otelPlugin instruments GORM with client spans.
//
// Why a local plugin instead of gorm.io/plugin/opentelemetry: that package
// imports the MySQL/ClickHouse/SQL Server drivers just to name the DB system,
// which would drag three unused database stacks into the template's dependency
// tree. The callback surface it needs is small enough to own.
type otelPlugin struct{}

func (otelPlugin) Name() string { return "otel-tracing" }

// Initialize registers a before/after pair per GORM operation. The concrete
// callback processor type is unexported, so the registration closures capture
// the specific processor instead of naming it.
func (otelPlugin) Initialize(db *gorm.DB) error {
	type hooks struct {
		operation string
		before    func(name string, fn func(*gorm.DB)) error
		after     func(name string, fn func(*gorm.DB)) error
	}
	pairs := []hooks{
		{
			operation: "create",
			before: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Create().Before("gorm:create").Register(name, fn)
			},
			after: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Create().After("gorm:create").Register(name, fn)
			},
		},
		{
			operation: "query",
			before: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Query().Before("gorm:query").Register(name, fn)
			},
			after: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Query().After("gorm:query").Register(name, fn)
			},
		},
		{
			operation: "update",
			before: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Update().Before("gorm:update").Register(name, fn)
			},
			after: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Update().After("gorm:update").Register(name, fn)
			},
		},
		{
			operation: "delete",
			before: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Delete().Before("gorm:delete").Register(name, fn)
			},
			after: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Delete().After("gorm:delete").Register(name, fn)
			},
		},
		{
			operation: "row",
			before: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Row().Before("gorm:row").Register(name, fn)
			},
			after: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Row().After("gorm:row").Register(name, fn)
			},
		},
		{
			operation: "raw",
			before: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Raw().Before("gorm:raw").Register(name, fn)
			},
			after: func(name string, fn func(*gorm.DB)) error {
				return db.Callback().Raw().After("gorm:raw").Register(name, fn)
			},
		},
	}

	for _, pair := range pairs {
		op := pair.operation
		// The after hook is registered first (GORM runs callbacks in registration
		// order) so the span already exists when the statement executes.
		if err := pair.after("otel:"+op+"_after", func(db *gorm.DB) { endSpan(db, op) }); err != nil {
			return fmt.Errorf("register %s after callback: %w", op, err)
		}
		if err := pair.before("otel:"+op+"_before", func(db *gorm.DB) { startSpan(db, op) }); err != nil {
			return fmt.Errorf("register %s before callback: %w", op, err)
		}
	}
	return nil
}

// instrumentDB attaches the span plugin. Exported through dbx.New; kept
// separate so tests can instrument a SQLite connection.
func instrumentDB(db *gorm.DB) error {
	if err := db.Use(otelPlugin{}); err != nil {
		return fmt.Errorf("attach gorm otel plugin: %w", err)
	}
	return nil
}

// spanKey is the InstanceSet key holding the in-flight span for one statement.
const spanKey = "dbx:otel:span"

func startSpan(db *gorm.DB, operation string) {
	if db.Statement == nil {
		return
	}
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, "db."+operation,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			attribute.String("db.system", db.Name()),
			attribute.String("db.operation", operation),
		),
	)
	db.Statement.Context = ctx
	db.InstanceSet(spanKey, span)
}

func endSpan(db *gorm.DB, operation string) {
	value, ok := db.InstanceGet(spanKey)
	if !ok {
		return
	}
	span, ok := value.(oteltrace.Span)
	if !ok {
		return
	}

	if db.Statement != nil {
		// Statement.SQL is built with placeholders; values stay out of traces.
		if sql := db.Statement.SQL.String(); sql != "" {
			span.SetAttributes(attribute.String("db.statement", sql))
		}
	}
	if db.RowsAffected > 0 {
		span.SetAttributes(attribute.Int64("db.rows_affected", db.RowsAffected))
	}
	if db.Error != nil {
		span.RecordError(db.Error)
		span.SetStatus(codes.Error, operation+" failed")
	}
	span.End()
}
