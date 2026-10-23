package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestCodeHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code Code
		want int
	}{
		{"ok", CodeOK, http.StatusOK},
		{"invalid argument", CodeInvalidArgument, http.StatusBadRequest},
		{"unauthorized", CodeUnauthorized, http.StatusUnauthorized},
		{"forbidden", CodeForbidden, http.StatusForbidden},
		{"not found", CodeNotFound, http.StatusNotFound},
		{"conflict", CodeConflict, http.StatusConflict},
		{"rate limited", CodeTooManyRequests, http.StatusTooManyRequests},
		{"internal", CodeInternal, http.StatusInternalServerError},
		{"database", CodeDatabase, http.StatusInternalServerError},
		{"unclassified 5xx bucket", Code(59999), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.code.HTTPStatus(); got != tt.want {
				t.Errorf("Code(%d).HTTPStatus() = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestFrom(t *testing.T) {
	t.Parallel()

	if got := From(nil); got != nil {
		t.Fatalf("From(nil) = %v, want nil", got)
	}

	typed := Wrap(CodeConflict, "dup", errors.New("boom"))
	if got := From(typed); got != typed {
		t.Errorf("From(typed) = %v, want the original typed error", got)
	}

	// A nested typed error is still recognized through errors.As.
	nested := fmt.Errorf("repo: %w", typed)
	if got := From(nested); got != typed {
		t.Errorf("From(nested) = %v, want the wrapped typed error", got)
	}

	// Unknown errors must be masked as internal, never leaked to clients.
	unknown := From(errors.New("secret dsn=pw"))
	if unknown.Code != CodeInternal {
		t.Errorf("From(unknown).Code = %d, want %d", unknown.Code, CodeInternal)
	}
	if unknown.Message != "internal server error" {
		t.Errorf("From(unknown).Message = %q, want generic message", unknown.Message)
	}
}

func TestSentinelsMatchByCode(t *testing.T) {
	t.Parallel()

	// Repository code wraps ErrNotFound with a specific message; callers must
	// still match the sentinel through errors.Is (ErrNotFound.Is compares Code).
	err := Wrap(CodeNotFound, "user 42 not found", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(wrapped not-found, ErrNotFound) = false, want true")
	}
	if errors.Is(err, ErrConflict) {
		t.Error("errors.Is(wrapped not-found, ErrConflict) = true, want false")
	}
	if !errors.Is(fmt.Errorf("repo: %w", err), ErrNotFound) {
		t.Error("errors.Is must traverse fmt.Errorf wrappers")
	}
}

func TestErrorUnwrapAndMessage(t *testing.T) {
	t.Parallel()

	cause := errors.New("driver: down")
	err := Wrap(CodeDatabase, "query failed", cause)
	if got, want := err.Error(), "query failed: driver: down"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap chain must expose the cause")
	}

	if got, want := New(CodeForbidden, "nope").Error(), "nope"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
