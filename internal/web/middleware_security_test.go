package web

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		hsts     bool
		wantHSTS string
	}{
		{name: "development omits hsts", hsts: false, wantHSTS: ""},
		{name: "production sends hsts", hsts: true, wantHSTS: "max-age=31536000; includeSubDomains"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := gin.New()
			r.Use(SecurityHeaders(tc.hsts))
			r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := serve(t, r, newRequest(t, http.MethodGet, "/"))
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
			if got := w.Header().Get("Strict-Transport-Security"); got != tc.wantHSTS {
				t.Errorf("Strict-Transport-Security = %q, want %q", got, tc.wantHSTS)
			}
		})
	}
}

func TestNoStore(t *testing.T) {
	t.Parallel()

	r := gin.New()
	r.Use(NoStore())
	r.GET("/", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"access_token": "x"}) })

	w := serve(t, r, newRequest(t, http.MethodGet, "/"))
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := w.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", got)
	}
}

// TestNoStoreSurvivesRejection 保证 429 之类的提前返回也带 no-store。
func TestNoStoreSurvivesRejection(t *testing.T) {
	t.Parallel()

	r := gin.New()
	r.Use(NoStore(), RateLimit(true, 1, 1))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := newRequest(t, http.MethodGet, "/")
	req.RemoteAddr = "198.51.100.77:1234"
	if w := serve(t, r, req); w.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", w.Code)
	}
	w := serve(t, r, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("429 Cache-Control = %q, want no-store", got)
	}
}
