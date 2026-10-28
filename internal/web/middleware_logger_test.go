package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSanitizedQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "allowlisted only", raw: "page=2&page_size=20", want: "page=2&page_size=20"},
		{name: "free text dropped", raw: "keyword=alice@example.com&page=2", want: "page=2"},
		{name: "unknown dropped", raw: "token=secret&unknown=1", want: ""},
		{name: "sorted and duplicates kept", raw: "status=1&page=3&status=2", want: "page=3&status=1&status=2"},
		{name: "unparsable dropped", raw: "page=%zz", want: ""},
		{name: "long value truncated", raw: "page=" + strings.Repeat("a", 128), want: "page=" + strings.Repeat("a", loggerQueryValueLimit)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizedQuery(tc.raw); got != tc.want {
				t.Fatalf("sanitizedQuery(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestAccessLogDropsFreeTextQuery pins the PII boundary: the access log keeps
// paging/filter keys for debugging but never writes user-supplied search text.
func TestAccessLogDropsFreeTextQuery(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	engine := gin.New()
	engine.Use(RequestID(), Logger(zap.New(core)))
	engine.GET("/api/v1/users", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users?keyword=alice%40example.com&page=2&token=secret", http.NoBody)
	request.RemoteAddr = "203.0.113.7:12345"
	engine.ServeHTTP(httptest.NewRecorder(), request)

	entries := logs.FilterMessage("http_request").All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	path, _ := entries[0].ContextMap()["path"].(string)
	if path != "/api/v1/users?page=2" {
		t.Fatalf("logged path = %q, want only the allowlisted key", path)
	}
}

// TestAccessLogCarriesTraceID keeps the log/trace correlation working: the
// response exposes X-Trace-Id and the access log carries the same id, so a
// user report can be pasted into Tempo.
func TestAccessLogCarriesTraceID(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	engine := gin.New()
	engine.Use(Logger(zap.New(core)))
	engine.GET("/api/v1/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
		TraceFlags: trace.FlagsSampled,
	})
	request := httptest.NewRequestWithContext(
		trace.ContextWithSpanContext(context.Background(), spanContext),
		http.MethodGet, "/api/v1/ping", http.NoBody,
	)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("X-Trace-Id"); got != traceID.String() {
		t.Fatalf("X-Trace-Id = %q, want %q", got, traceID.String())
	}
	entries := logs.FilterMessage("http_request").All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if got, _ := entries[0].ContextMap()["trace_id"].(string); got != traceID.String() {
		t.Fatalf("logged trace_id = %q, want %q", got, traceID.String())
	}
}
