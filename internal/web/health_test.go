package web

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func TestHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pingErr    error
		path       string
		wantStatus int
	}{
		{name: "liveness is independent of the db", pingErr: errors.New("down"), path: "/healthz", wantStatus: http.StatusOK},
		{name: "readiness ok", path: "/readyz", wantStatus: http.StatusOK},
		{name: "readiness db down", pingErr: errors.New("connection refused"), path: "/readyz", wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := NewHealth(fakePinger{err: tt.pingErr})
			r := gin.New()
			r.GET("/healthz", h.Liveness)
			r.GET("/readyz", h.Readiness)

			w := serve(t, r, newRequest(t, http.MethodGet, tt.path))
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
