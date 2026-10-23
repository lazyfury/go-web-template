package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
)

func newTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	return c, w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) Body {
	t.Helper()
	var body Body
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, w.Body.String())
	}
	return body
}

func TestSuccessHelpers(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()
		c, w := newTestContext(t)
		OK(c, map[string]string{"hello": "world"})
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		body := decodeBody(t, w)
		if body.Code != apperr.CodeOK || body.Message != "ok" {
			t.Errorf("body = %+v, want code 0 / message ok", body)
		}
	})

	t.Run("Created", func(t *testing.T) {
		t.Parallel()
		c, w := newTestContext(t)
		Created(c, map[string]int{"id": 1})
		if w.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201", w.Code)
		}
		if got := decodeBody(t, w).Message; got != "created" {
			t.Errorf("message = %q, want created", got)
		}
	})

	t.Run("NoContent", func(t *testing.T) {
		t.Parallel()
		c, w := newTestContext(t)
		NoContent(c)
		c.Writer.WriteHeaderNow() // gin writes the header on return in production; flush for the recorder
		if w.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", w.Body.String())
		}
	})
}

func TestFail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   apperr.Code
		wantMsg    string
	}{
		{
			name:       "typed conflict",
			err:        apperr.Wrap(apperr.CodeConflict, "email already registered", errors.New("23505")),
			wantStatus: http.StatusConflict,
			wantCode:   apperr.CodeConflict,
			wantMsg:    "email already registered",
		},
		{
			name:       "sentinel not found",
			err:        apperr.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   apperr.CodeNotFound,
			wantMsg:    "resource not found",
		},
		{
			name:       "unknown error is masked",
			err:        errors.New("pq: password authentication failed"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   apperr.CodeInternal,
			wantMsg:    "internal server error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, w := newTestContext(t)
			Fail(c, tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			body := decodeBody(t, w)
			if body.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", body.Code, tt.wantCode)
			}
			if body.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", body.Message, tt.wantMsg)
			}
			if body.Data != nil {
				t.Errorf("data = %v, want omitted on error", body.Data)
			}
		})
	}
}
