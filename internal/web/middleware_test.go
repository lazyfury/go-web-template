package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/config"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func serve(t *testing.T, r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(context.Background(), method, target, http.NoBody)
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	var seen string
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) {
		seen = RequestIDFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	t.Run("reuses inbound id", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/")
		req.Header.Set(RequestIDHeader, "given-id")
		w := serve(t, r, req)
		if got := w.Header().Get(RequestIDHeader); got != "given-id" {
			t.Errorf("response %s = %q, want given-id", RequestIDHeader, got)
		}
		if seen != "given-id" {
			t.Errorf("context id = %q, want given-id", seen)
		}
	})

	t.Run("generates when absent", func(t *testing.T) {
		w := serve(t, r, newRequest(t, http.MethodGet, "/"))
		header := w.Header().Get(RequestIDHeader)
		if header == "" {
			t.Fatal("expected generated request id")
		}
		if seen != header {
			t.Errorf("context id = %q, response header = %q; want equal", seen, header)
		}
	})

	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("RequestIDFromContext(empty) = %q, want empty", got)
	}
}

func TestCORS(t *testing.T) {
	t.Parallel()

	corsCfg := &config.CORS{
		AllowedOrigins:   []string{"https://admin.example.com"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost},
		AllowedHeaders:   []string{"Content-Type", "X-Request-ID"},
		AllowCredentials: true,
	}
	r := gin.New()
	r.Use(CORS(corsCfg))
	r.POST("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	t.Run("allowed origin preflight", func(t *testing.T) {
		req := newRequest(t, http.MethodOptions, "/")
		req.Header.Set("Origin", "https://admin.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		w := serve(t, r, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
			t.Errorf("allow-origin = %q, want the configured origin", got)
		}
		if w.Code != http.StatusNoContent {
			t.Errorf("preflight status = %d, want 204", w.Code)
		}
	})

	t.Run("disallowed origin gets no allow-origin header", func(t *testing.T) {
		req := newRequest(t, http.MethodOptions, "/")
		req.Header.Set("Origin", "https://evil.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		w := serve(t, r, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("allow-origin = %q, want empty for disallowed origin", got)
		}
	})

	t.Run("empty config falls back to wildcard", func(t *testing.T) {
		r := gin.New()
		r.Use(CORS(&config.CORS{}))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		req := newRequest(t, http.MethodGet, "/")
		req.Header.Set("Origin", "https://any.example.com")
		w := serve(t, r, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("allow-origin = %q, want *", got)
		}
	})
}

func TestRateLimit(t *testing.T) {
	t.Parallel()

	newRouter := func(enabled bool, rps float64, burst int) *gin.Engine {
		r := gin.New()
		r.Use(RateLimit(enabled, rps, burst))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		return r
	}

	t.Run("disabled never blocks", func(t *testing.T) {
		t.Parallel()
		r := newRouter(false, 1, 1)
		for i := 0; i < 3; i++ {
			if w := serve(t, r, newRequest(t, http.MethodGet, "/")); w.Code != http.StatusOK {
				t.Fatalf("request %d status = %d, want 200", i, w.Code)
			}
		}
	})

	t.Run("enabled returns 429 with retry-after", func(t *testing.T) {
		t.Parallel()
		r := newRouter(true, 1, 1)
		req := newRequest(t, http.MethodGet, "/")
		req.RemoteAddr = "203.0.113.7:4321" // same bucket across calls
		if w := serve(t, r, req); w.Code != http.StatusOK {
			t.Fatalf("first status = %d, want 200", w.Code)
		}

		w := serve(t, r, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("second status = %d, want 429", w.Code)
		}
		if got := w.Header().Get("Retry-After"); got != "1" {
			t.Errorf("retry-after = %q, want 1", got)
		}
		var body struct {
			Code apperr.Code `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Code != apperr.CodeTooManyRequests {
			t.Errorf("code = %d, want %d", body.Code, apperr.CodeTooManyRequests)
		}
	})
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	t.Run("injects a deadline into the request context", func(t *testing.T) {
		t.Parallel()
		r := gin.New()
		r.Use(Timeout(50 * time.Millisecond))

		var (
			hasDeadline bool
			remaining   time.Duration
		)
		r.GET("/", func(c *gin.Context) {
			deadline, ok := c.Request.Context().Deadline()
			hasDeadline = ok
			if ok {
				remaining = time.Until(deadline)
			}
			c.Status(http.StatusOK)
		})

		if w := serve(t, r, newRequest(t, http.MethodGet, "/")); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if !hasDeadline {
			t.Fatal("handler context has no deadline")
		}
		if remaining <= 0 || remaining > 50*time.Millisecond {
			t.Errorf("remaining = %v, want (0, 50ms]", remaining)
		}
	})

	t.Run("non-positive duration is a no-op", func(t *testing.T) {
		t.Parallel()
		r := gin.New()
		r.Use(Timeout(0))

		var hasDeadline bool
		r.GET("/", func(c *gin.Context) {
			_, hasDeadline = c.Request.Context().Deadline()
			c.Status(http.StatusOK)
		})
		serve(t, r, newRequest(t, http.MethodGet, "/"))
		if hasDeadline {
			t.Error("expected no deadline when duration is zero")
		}
	})
}

func TestRecovery(t *testing.T) {
	t.Parallel()

	r := gin.New()
	r.Use(Recovery(zap.NewNop()))
	r.GET("/panic", func(*gin.Context) { panic("boom") })

	w := serve(t, r, newRequest(t, http.MethodGet, "/panic"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var body struct {
		Code    apperr.Code `json:"code"`
		Message string      `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != apperr.CodeInternal {
		t.Errorf("code = %d, want %d", body.Code, apperr.CodeInternal)
	}
	if body.Message != "internal server error" {
		t.Errorf("message = %q, want generic message (no panic details)", body.Message)
	}
}

// TestMetricsUsesRoutePattern guards against label cardinality explosions:
// the "path" label must be the route pattern, not the raw URL.
func TestMetricsUsesRoutePattern(t *testing.T) {
	r := gin.New()
	r.Use(Metrics())
	r.GET("/api/v1/widgets/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

	serve(t, r, newRequest(t, http.MethodGet, "/api/v1/widgets/42"))
	serve(t, r, newRequest(t, http.MethodGet, "/api/v1/widgets/43"))

	got := counterValue(t, "http_requests_total", map[string]string{
		"method": http.MethodGet,
		"path":   "/api/v1/widgets/:id",
		"status": "200",
	})
	if got < 2 {
		t.Errorf("http_requests_total{path=/api/v1/widgets/:id} = %v, want >= 2 (labels must use the route pattern)", got)
	}
	if raw := counterValue(t, "http_requests_total", map[string]string{
		"method": http.MethodGet,
		"path":   "/api/v1/widgets/42",
		"status": "200",
	}); raw != 0 {
		t.Errorf("raw path label recorded (%v); cardinality would explode", raw)
	}
}

func counterValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			matched := true
			for k, want := range labels {
				found := false
				for _, pair := range metric.GetLabel() {
					if pair.GetName() == k && pair.GetValue() == want {
						found = true
						break
					}
				}
				if !found {
					matched = false
					break
				}
			}
			if matched {
				if c := metric.GetCounter(); c != nil {
					return c.GetValue()
				}
			}
		}
	}
	return 0
}
