package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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

// TestReadinessReportsSchemaDrift pins the migration check: ping alone is not
// readiness, because an out-of-date schema answers 200 on /readyz and 500 on
// every business request.
func TestReadinessReportsSchemaDrift(t *testing.T) {
	tests := []struct {
		name       string
		status     SchemaStatus
		probeErr   error
		wantStatus int
		wantReason string
	}{
		{
			name:       "current schema",
			status:     SchemaStatus{Applied: 7, Required: 7, Found: true, CoreTables: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "autocreate without migrations table",
			status:     SchemaStatus{Required: 7, CoreTables: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "schema behind",
			status:     SchemaStatus{Applied: 6, Required: 7, Found: true, CoreTables: true},
			wantStatus: http.StatusServiceUnavailable,
			wantReason: "schema behind",
		},
		{
			name:       "schema dirty",
			status:     SchemaStatus{Applied: 7, Required: 7, Found: true, Dirty: true, CoreTables: true},
			wantStatus: http.StatusServiceUnavailable,
			wantReason: "dirty",
		},
		{
			name:       "never migrated",
			status:     SchemaStatus{Required: 7},
			wantStatus: http.StatusServiceUnavailable,
			wantReason: "not applied",
		},
		{
			name:       "probe error",
			probeErr:   errors.New("catalog unavailable"),
			wantStatus: http.StatusServiceUnavailable,
			wantReason: "schema status unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := NewHealth(fakePinger{}, WithSchemaProbe(func(context.Context) (SchemaStatus, error) {
				return tt.status, tt.probeErr
			}))
			r := gin.New()
			r.GET("/readyz", h.Readiness)

			w := serve(t, r, newRequest(t, http.MethodGet, "/readyz"))
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantReason != "" {
				var body struct {
					Reason string `json:"reason"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if !strings.Contains(body.Reason, tt.wantReason) {
					t.Errorf("reason = %q, want to contain %q", body.Reason, tt.wantReason)
				}
			}
		})
	}
}
