// Package otelx initializes the OpenTelemetry tracer provider.
//
// Tracing is deliberately opt-in: the template must run with nothing but
// PostgreSQL, and an unreachable collector must never turn into a startup
// failure. When disabled, every span call stays on the no-op provider.
//
// Lifecycle follows ADR 14: the provider is built and shut down by cmd/server,
// which also owns the root goroutine. Internal packages only create spans.
package otelx

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

// exportTimeout bounds one OTLP export attempt: a stalled collector must not
// hold spans (or shutdown) open for the gRPC default.
const exportTimeout = 5 * time.Second

// Options is the narrow view of config.OTel this package needs.
type Options struct {
	Enabled     bool
	ServiceName string
	Version     string
	Endpoint    string
	Insecure    bool
	SampleRatio float64
}

// Provider owns the tracer provider. A disabled Provider is valid and nil-safe.
type Provider struct {
	tp     *sdktrace.TracerProvider
	logger *zap.Logger
}

// Option customizes New (tests inject an in-memory exporter).
type Option func(*options)

type options struct {
	exporter sdktrace.SpanExporter
}

// WithSpanExporter replaces the OTLP exporter. Tests use it to assert that
// spans are produced without opening a network connection.
func WithSpanExporter(exporter sdktrace.SpanExporter) Option {
	return func(o *options) { o.exporter = exporter }
}

// New builds the provider and installs it as the global one. It returns a
// no-op Provider when tracing is disabled.
func New(ctx context.Context, opts Options, logger *zap.Logger, extra ...Option) (*Provider, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if !opts.Enabled {
		return &Provider{logger: logger}, nil
	}

	cfg := options{}
	for _, opt := range extra {
		opt(&cfg)
	}
	exporter := cfg.exporter
	if exporter == nil {
		clientOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(opts.Endpoint),
			otlptracegrpc.WithTimeout(exportTimeout),
		}
		if opts.Insecure {
			clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
		}
		var err error
		exporter, err = otlptracegrpc.New(ctx, clientOpts...)
		if err != nil {
			return nil, err
		}
	}

	if opts.ServiceName == "" {
		opts.ServiceName = "go-web-template"
	}
	res := resource.NewWithAttributes(
		"",
		attribute.String("service.name", opts.ServiceName),
		attribute.String("service.version", opts.Version),
	)

	ratio := opts.SampleRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 1.0
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// ParentBased keeps a sampled trace sampled across services; the ratio
		// only applies at the root.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	// Export failures are operational noise, not app errors: log them through
	// zap (rate-limited by the exporter's own batching) instead of stderr.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("otel exporter error", zap.Error(err))
	}))

	return &Provider{tp: tp, logger: logger}, nil
}

// Shutdown flushes buffered spans and stops the provider. The caller must pass
// a bounded context: cmd/server uses a fresh 5s timeout because the signal
// context is already canceled by then.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.tp == nil {
		return nil
	}
	if err := p.tp.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
