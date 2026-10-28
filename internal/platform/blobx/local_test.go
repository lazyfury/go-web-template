package blobx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestLocal(t *testing.T) *Local {
	t.Helper()
	dir := t.TempDir()
	store, err := NewLocal(dir, "http://localhost:8080", []byte("test-secret-0123456789abcdef0123"))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestValidateKey(t *testing.T) {
	good := []string{"file/202601/abc.png", "a", "file/202601/x.txt"}
	for _, key := range good {
		if err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q) = %v, want nil", key, err)
		}
	}
	bad := []string{"", "/etc/passwd", "a/../b", "a/./b", "a//b", `a\b`, "a/\x00b", strings.Repeat("x", 1025)}
	for _, key := range bad {
		if err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) = nil, want error", key)
		} else if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ValidateKey(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
}

func TestLocalPutOpenStatDelete(t *testing.T) {
	store := newTestLocal(t)
	ctx := context.Background()
	payload := []byte("hello blobx")

	info, err := store.Put(ctx, "file/202601/a.txt", bytes.NewReader(payload), int64(len(payload)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.Size != int64(len(payload)) || info.Key != "file/202601/a.txt" || info.ContentType != "text/plain" {
		t.Fatalf("Put info = %+v", info)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "file", "202601", "a.txt")); err != nil {
		t.Fatalf("object not on disk: %v", err)
	}

	reader, opened, err := store.Open(ctx, "file/202601/a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("content = %q, want %q", got, payload)
	}
	if opened.Size != int64(len(payload)) {
		t.Errorf("opened size = %d", opened.Size)
	}
	// The reader must be seekable: the HTTP layer uses http.ServeContent for ranges.
	if _, ok := reader.(io.ReadSeeker); !ok {
		t.Errorf("local Open reader is not an io.ReadSeeker")
	}

	stat, err := store.Stat(ctx, "file/202601/a.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.Size != int64(len(payload)) {
		t.Errorf("stat size = %d", stat.Size)
	}

	if err := store.Delete(ctx, "file/202601/a.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Stat(ctx, "file/202601/a.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat after delete = %v, want ErrNotFound", err)
	}
	// Deleting twice is fine (purge jobs are idempotent).
	if err := store.Delete(ctx, "file/202601/a.txt"); err != nil {
		t.Errorf("Delete missing = %v, want nil", err)
	}
}

func TestLocalPutRejectsWrongSize(t *testing.T) {
	store := newTestLocal(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, "file/a.txt", strings.NewReader("short"), 99, "text/plain"); !errors.Is(err, ErrSizeMismatch) {
		t.Errorf("short stream error = %v, want ErrSizeMismatch", err)
	}
	if _, err := store.Put(ctx, "file/b.txt", strings.NewReader("longer than declared"), 4, "text/plain"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("long stream error = %v, want ErrTooLarge", err)
	}
	// A failed upload must not leave an object (or a temp file) behind.
	if _, err := store.Stat(ctx, "file/a.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat after failed Put = %v, want ErrNotFound", err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root(), ".tmp"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir has %d leftover files", len(entries))
	}
}

func TestLocalPutRejectsTraversal(t *testing.T) {
	store := newTestLocal(t)
	if _, err := store.Put(context.Background(), "../escape.txt", strings.NewReader("x"), 1, "text/plain"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Put with traversal key = %v, want ErrInvalidKey", err)
	}
}

func TestLocalSignedURL(t *testing.T) {
	store := newTestLocal(t)
	ctx := context.Background()
	secret := []byte("test-secret-0123456789abcdef0123")

	url, expires, err := store.URL(ctx, "file/202601/a.txt", SignedURLOptions{
		Expiry:      time.Minute,
		Disposition: "attachment",
		PathID:      "42",
	})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if !strings.HasPrefix(url, "http://localhost:8080/files/42/") || !strings.HasSuffix(url, "/"+SignLocalURL(secret, "file/202601/a.txt", expires.Unix(), "attachment")) {
		t.Fatalf("unexpected URL %q", url)
	}
	if time.Until(expires) > time.Minute+time.Second || time.Until(expires) < time.Minute-time.Second {
		t.Errorf("expiry %v is not ~1m", time.Until(expires))
	}

	exp := expires.Unix()
	sig := SignLocalURL(secret, "file/202601/a.txt", exp, "attachment")
	if err := VerifyLocalURL(secret, "file/202601/a.txt", exp, "attachment", sig, time.Now()); err != nil {
		t.Errorf("VerifyLocalURL(valid) = %v", err)
	}
	if err := VerifyLocalURL(secret, "file/202601/a.txt", exp, "inline", sig, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("VerifyLocalURL(changed disposition) = %v, want ErrNotFound", err)
	}
	if err := VerifyLocalURL(secret, "file/202601/other.txt", exp, "attachment", sig, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("VerifyLocalURL(changed key) = %v, want ErrNotFound", err)
	}
	if err := VerifyLocalURL([]byte("another-secret"), "file/202601/a.txt", exp, "attachment", sig, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("VerifyLocalURL(wrong secret) = %v, want ErrNotFound", err)
	}
	if err := VerifyLocalURL(secret, "file/202601/a.txt", exp, "attachment", sig, expires.Add(time.Second)); !errors.Is(err, ErrNotFound) {
		t.Errorf("VerifyLocalURL(expired) = %v, want ErrNotFound", err)
	}

	if _, _, err := store.URL(ctx, "file/a.txt", SignedURLOptions{}); err == nil {
		t.Error("URL without PathID = nil error, want error")
	}
}

func TestNewLocalValidation(t *testing.T) {
	if _, err := NewLocal(t.TempDir(), "http://localhost:8080", nil); err == nil {
		t.Error("NewLocal without secret = nil error")
	}
	if _, err := NewLocal(t.TempDir(), "", []byte("secret")); err == nil {
		t.Error("NewLocal without baseURL = nil error")
	}
}

func TestLocalOpenMissing(t *testing.T) {
	store := newTestLocal(t)
	if _, _, err := store.Open(context.Background(), "file/nope.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open missing = %v, want ErrNotFound", err)
	}
	if _, err := store.Stat(context.Background(), "file/nope.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat missing = %v, want ErrNotFound", err)
	}
}
