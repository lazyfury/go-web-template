package dbx

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/example/go-web-template/internal/testutil"
)

// TestInstrumentDB pins both the wiring and the span shape: a query must run,
// emit one client span, and keep values out of the statement attribute.
func TestInstrumentDB(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	db := testutil.NewSQLiteDB(t)
	if err := instrumentDB(db); err != nil {
		t.Fatalf("instrumentDB: %v", err)
	}

	var one int
	if err := db.WithContext(context.Background()).Raw("SELECT 1").Scan(&one).Error; err != nil {
		t.Fatalf("query after instrumentation: %v", err)
	}
	if one != 1 {
		t.Fatalf("SELECT 1 = %d", one)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Name != "db.row" {
		t.Errorf("span name = %q, want db.row (Scan goes through the Row callbacks)", spans[0].Name)
	}
	attrs := map[string]string{}
	for _, attr := range spans[0].Attributes {
		attrs[string(attr.Key)] = attr.Value.String()
	}
	if attrs["db.system"] != "sqlite" || attrs["db.operation"] != "row" {
		t.Errorf("attributes = %v", attrs)
	}
	if _, ok := attrs["db.statement"]; !ok {
		t.Errorf("missing db.statement: %v", attrs)
	}
}

// TestInstrumentDBRecordsErrors keeps failed statements visible in the trace
// instead of looking like an empty query.
func TestInstrumentDBRecordsErrors(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	db := testutil.NewSQLiteDB(t)
	if err := instrumentDB(db); err != nil {
		t.Fatalf("instrumentDB: %v", err)
	}
	if err := db.WithContext(context.Background()).Exec("SELECT * FROM no_such_table").Error; err == nil {
		t.Fatal("query should fail")
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("span status = %v, want error", spans[0].Status)
	}
	if len(spans[0].Events) == 0 {
		t.Error("failed span has no recorded error event")
	}
}
