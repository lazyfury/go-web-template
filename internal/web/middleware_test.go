package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/httpx"
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
		seen = httpx.RequestIDFromContext(c.Request.Context())
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

	if got := httpx.RequestIDFromContext(context.Background()); got != "" {
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

// ipLimit is the shape every IP-keyed guard has: a registry-owned token bucket
// per client IP. Tests use it instead of a self-starting limiter.
func ipLimit(rps float64, burst int) gin.HandlerFunc {
	return NewLimiters().Middleware("test", true, clientIPKey, rps, burst)
}

// TestRateLimitIgnoresSpoofedXFF 钉住 F2：没有配置可信代理时，调用方不能靠轮换
// X-Forwarded-For 换到新的限流桶（否则登录限流形同虚设）。
func TestRateLimitIgnoresSpoofedXFF(t *testing.T) {
	t.Parallel()

	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	r.Use(ipLimit(1, 1))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	first := newRequest(t, http.MethodGet, "/")
	first.RemoteAddr = "203.0.113.9:1111"
	first.Header.Set("X-Forwarded-For", "10.0.0.1")
	if w := serve(t, r, first); w.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", w.Code)
	}

	// 同一个真实客户端（同 IP、不同源端口），伪造不同的 XFF。
	second := newRequest(t, http.MethodGet, "/")
	second.RemoteAddr = "203.0.113.9:2222"
	second.Header.Set("X-Forwarded-For", "10.0.0.2")
	if w := serve(t, r, second); w.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For escaped the bucket: status = %d, want 429", w.Code)
	}
}

// TestRateLimitUsesXFFFromTrustedProxy 保证反代部署仍然按真实客户端 IP 限流。
func TestRateLimitUsesXFFFromTrustedProxy(t *testing.T) {
	t.Parallel()

	r := gin.New()
	if err := r.SetTrustedProxies([]string{"10.1.0.0/16"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	r.Use(ipLimit(1, 1))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	viaProxy := func(client string) *http.Request {
		req := newRequest(t, http.MethodGet, "/")
		req.RemoteAddr = "10.1.0.5:9999" // 可信反代
		req.Header.Set("X-Forwarded-For", client)
		return req
	}

	for _, client := range []string{"198.51.100.1", "198.51.100.2"} {
		if w := serve(t, r, viaProxy(client)); w.Code != http.StatusOK {
			t.Fatalf("client %s status = %d, want 200 (own bucket)", client, w.Code)
		}
	}
	if w := serve(t, r, viaProxy("198.51.100.1")); w.Code != http.StatusTooManyRequests {
		t.Fatalf("repeat client status = %d, want 429", w.Code)
	}
}

func TestRateLimit(t *testing.T) {
	t.Parallel()

	newRouter := func(enabled bool, rps float64, burst int) *gin.Engine {
		r := gin.New()
		r.Use(NewLimiters().Middleware("test", enabled, clientIPKey, rps, burst))
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

	// Unknown routes must collapse into one fixed label instead of the raw path.
	serve(t, r, newRequest(t, http.MethodGet, "/scanner/probe/123"))
	if got := counterValue(t, "http_requests_total", map[string]string{
		"method": http.MethodGet,
		"path":   unmatchedPath,
		"status": "404",
	}); got < 1 {
		t.Errorf("unmatched requests not counted under %q (got %v)", unmatchedPath, got)
	}
	if raw := counterValue(t, "http_requests_total", map[string]string{
		"method": http.MethodGet,
		"path":   "/scanner/probe/123",
	}); raw != 0 {
		t.Errorf("raw unmatched path recorded (%v); cardinality would explode", raw)
	}
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()

	newRouter := func(maxBytes int64) *gin.Engine {
		r := gin.New()
		r.Use(BodyLimit(maxBytes))
		r.POST("/", func(c *gin.Context) {
			_, _ = c.Request.Body.Read(make([]byte, 1))
			c.Status(http.StatusOK)
		})
		return r
	}

	t.Run("rejects declared oversize with 413", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/",
			strings.NewReader(`{"name":"this body is definitely larger than 16 bytes"}`))
		req.Header.Set("Content-Type", "application/json")
		w := serve(t, newRouter(16), req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", w.Code)
		}
		var body struct {
			Code apperr.Code `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Code != apperr.CodePayloadTooLarge {
			t.Errorf("code = %d, want %d", body.Code, apperr.CodePayloadTooLarge)
		}
	})

	t.Run("allows bodies under the limit", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/",
			strings.NewReader(`{"a":1}`))
		if w := serve(t, newRouter(1024), req); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("zero disables the limit", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/",
			strings.NewReader(`{"a":"`+strings.Repeat("x", 2048)+`"}`))
		if w := serve(t, newRouter(0), req); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})
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

// TestRequestIDRejectsClientSuppliedJunk 钉住 F11：客户端只能提供格式合法的
// X-Request-Id，否则服务端重新生成（避免伪造关联 id / 往日志里塞任意文本）。
func TestRequestIDRejectsClientSuppliedJunk(t *testing.T) {
	t.Parallel()

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(RequestID())
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		return r
	}

	t.Run("well formed id is preserved", func(t *testing.T) {
		t.Parallel()
		r := newRouter()
		req := newRequest(t, http.MethodGet, "/")
		req.Header.Set(RequestIDHeader, "3f1a9c2e-7b44-4d0f-9a5e-0d1c2b3a4f50")
		if got := serve(t, r, req).Header().Get(RequestIDHeader); got != "3f1a9c2e-7b44-4d0f-9a5e-0d1c2b3a4f50" {
			t.Errorf("request id = %q, want the client value", got)
		}
	})

	for name, junk := range map[string]string{
		"spaces":   "id with spaces",
		"newline":  "abc\ndef",
		"quotes":   `"><script>`,
		"unicode":  "ｕｕｉｄ",
		"too long": strings.Repeat("a", maxRequestIDLen+1),
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			t.Parallel()
			r := newRouter()
			req := newRequest(t, http.MethodGet, "/")
			req.Header.Set(RequestIDHeader, junk)
			got := serve(t, r, req).Header().Get(RequestIDHeader)
			if got == junk {
				t.Fatalf("junk request id %q was accepted", junk)
			}
			if _, err := uuid.Parse(got); err != nil {
				t.Errorf("regenerated id %q is not a uuid: %v", got, err)
			}
		})
	}
}

// TestRateLimitKeyed 验证按调用方给定的维度分桶：不同 key 互不影响，同 key 共享桶。
func TestRateLimitKeyed(t *testing.T) {
	t.Parallel()

	r := gin.New()
	r.Use(NewLimiters().Middleware("keyed", true, func(c *gin.Context) string { return c.GetHeader("X-Test-Key") }, 1, 1))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	send := func(key string) int {
		req := newRequest(t, http.MethodGet, "/")
		req.RemoteAddr = "203.0.113.20:1234" // 同一个 IP，靠 key 区分
		req.Header.Set("X-Test-Key", key)
		return serve(t, r, req).Code
	}

	if code := send("user:1"); code != http.StatusOK {
		t.Fatalf("first user:1 = %d, want 200", code)
	}
	if code := send("user:2"); code != http.StatusOK {
		t.Fatalf("user:2 must have its own bucket, got %d", code)
	}
	if code := send("user:1"); code != http.StatusTooManyRequests {
		t.Fatalf("second user:1 = %d, want 429", code)
	}
}

// TestRateLimitRetryAfterFollowsBucketSpeed pins the hint: a bucket that refills
// one token every 2s must tell the client 2s, not a constant 1s.
func TestRateLimitRetryAfterFollowsBucketSpeed(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(NewLimiters().Middleware("slow", true, clientIPKey, 0.5, 1))
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := newRequest(t, http.MethodGet, "/")
	request.RemoteAddr = "198.51.100.9:4321"
	if w := serve(t, engine, request); w.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", w.Code)
	}
	w := serve(t, engine, request)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("retry-after = %q, want 2 (rps=0.5)", got)
	}
}
