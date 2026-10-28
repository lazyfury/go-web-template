package otelx

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
)

func TestNewDisabledIsNoop(t *testing.T) {
	provider, err := New(context.Background(), Options{}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// TestNewExportsSpans pins the whole wiring: enabling the provider installs it
// globally, so an unrelated package calling otel.Tracer (dbx, otelgin) produces
// spans that are exported and carry the service resource.
func TestNewExportsSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider, err := New(context.Background(), Options{
		Enabled:     true,
		ServiceName: "unit-test",
		Version:     "v9.9.9",
		SampleRatio: 1,
	}, zap.NewNop(), WithSpanExporter(exporter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, span := otel.Tracer("test").Start(context.Background(), "unit-span")
	span.SetAttributes(attribute.String("test.key", "value"))
	span.End()

	// Flush before reading: tracetest's exporter clears its buffer on Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := provider.tp.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Name != "unit-span" {
		t.Errorf("span name = %q", spans[0].Name)
	}

	attrs := map[string]string{}
	for _, attr := range spans[0].Resource.Attributes() {
		attrs[string(attr.Key)] = attr.Value.String()
	}
	if attrs["service.name"] != "unit-test" || attrs["service.version"] != "v9.9.9" {
		t.Errorf("resource attributes = %v", attrs)
	}
}

func TestSampleRatioFallback(t *testing.T) {
	// An invalid ratio must not silently disable tracing (0 would sample
	// nothing); New falls back to 1.0 so local debugging always sees spans.
	exporter := tracetest.NewInMemoryExporter()
	provider, err := New(context.Background(), Options{
		Enabled: true, ServiceName: "ratio", SampleRatio: 0,
	}, zap.NewNop(), WithSpanExporter(exporter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, span := otel.Tracer("test").Start(context.Background(), "ratio-span")
	span.End()
	if err := provider.tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
	if got := len(exporter.GetSpans()); got != 1 {
		t.Fatalf("spans = %d, want 1 with the 1.0 fallback", got)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
