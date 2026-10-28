package blobx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/config"
)

// fakeS3 is a minimal path-style S3 endpoint: enough for Put/Get/Head/Delete
// and for presigned URL assertions. It deliberately does not validate
// signatures — the SDK's own signing is covered by the presign assertions.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]fakeObject
}

type fakeObject struct {
	body        []byte
	contentType string
}

func newFakeS3(t *testing.T) (*fakeS3, *httptest.Server) {
	t.Helper()
	fake := &fakeS3{objects: make(map[string]fakeObject)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path style: /<bucket>/<key...>
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		if len(parts) != 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		key, err := url.PathUnescape(parts[1])
		if err != nil {
			http.Error(w, "bad key", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, "read body", http.StatusBadRequest)
				return
			}
			fake.objects[key] = fakeObject{body: body, contentType: r.Header.Get("Content-Type")}
			w.Header().Set("ETag", `"fake-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			object, ok := fake.objects[key]
			if !ok {
				writeFakeS3NotFound(w)
				return
			}
			if object.contentType != "" {
				w.Header().Set("Content-Type", object.contentType)
			}
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.Header().Set("Content-Length", itoa(len(object.body)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(object.body)
		case http.MethodHead:
			object, ok := fake.objects[key]
			if !ok {
				writeFakeS3NotFound(w)
				return
			}
			if object.contentType != "" {
				w.Header().Set("Content-Type", object.contentType)
			}
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.Header().Set("Content-Length", itoa(len(object.body)))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			delete(fake.objects, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	return fake, server
}

func writeFakeS3NotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
}

func itoa(n int) string { return strconv.Itoa(n) }

func newTestS3(t *testing.T) (*S3, *fakeS3) {
	t.Helper()
	fake, server := newFakeS3(t)
	store, err := NewS3(context.Background(), &S3Config{
		Endpoint:     server.URL,
		Region:       "us-east-1",
		Bucket:       "test-bucket",
		AccessKey:    "test-access-key",
		SecretKey:    "test-secret-key",
		UsePathStyle: true,
		PresignTTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return store, fake
}

func TestS3PutOpenStatDelete(t *testing.T) {
	store, fake := newTestS3(t)
	ctx := context.Background()
	payload := []byte("s3 payload")

	info, err := store.Put(ctx, "file/202601/a.txt", bytes.NewReader(payload), int64(len(payload)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.Key != "file/202601/a.txt" || info.Size != int64(len(payload)) {
		t.Fatalf("Put info = %+v", info)
	}
	fake.mu.Lock()
	stored, ok := fake.objects["file/202601/a.txt"]
	fake.mu.Unlock()
	if !ok || !bytes.Equal(stored.body, payload) {
		t.Fatalf("object not stored, got %+v ok=%v", stored, ok)
	}

	reader, opened, err := store.Open(ctx, "file/202601/a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("Open content = %q err=%v", got, err)
	}
	if opened.Size != int64(len(payload)) || opened.ContentType != "text/plain" {
		t.Errorf("Open info = %+v", opened)
	}

	stat, err := store.Stat(ctx, "file/202601/a.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.Size != int64(len(payload)) {
		t.Errorf("Stat size = %d", stat.Size)
	}

	if err := store.Delete(ctx, "file/202601/a.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Stat(ctx, "file/202601/a.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat after delete = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "file/202601/a.txt"); err != nil {
		t.Errorf("Delete missing = %v, want nil", err)
	}
}

func TestS3PresignedURL(t *testing.T) {
	store, _ := newTestS3(t)
	signed, expires, err := store.URL(context.Background(), "file/202601/a.txt", SignedURLOptions{
		Expiry:      5 * time.Minute,
		Disposition: "attachment",
		Filename:    "我的 报表.pdf",
	})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if !strings.Contains(signed, "/test-bucket/file/202601/a.txt") {
		t.Errorf("presigned URL %q does not address the object", signed)
	}
	if !strings.Contains(signed, "X-Amz-Signature=") {
		t.Errorf("presigned URL %q has no signature", signed)
	}
	if !strings.Contains(signed, "response-content-disposition=") {
		t.Errorf("presigned URL %q does not pin the disposition", signed)
	}
	if time.Until(expires) > 5*time.Minute+time.Second {
		t.Errorf("expiry %v is longer than requested", time.Until(expires))
	}
}

func TestS3Validation(t *testing.T) {
	if _, err := NewS3(context.Background(), &S3Config{Region: "us-east-1"}); err == nil {
		t.Error("NewS3 without bucket = nil error")
	}
	store, _ := newTestS3(t)
	if _, err := store.Put(context.Background(), "../x", strings.NewReader("x"), 1, "text/plain"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Put traversal = %v, want ErrInvalidKey", err)
	}
}

func TestFactoryBuildsConfiguredDriver(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Local.Root = t.TempDir()
	store, err := New(context.Background(), &cfg.Storage)
	if err != nil {
		t.Fatalf("New(local): %v", err)
	}
	if store.Driver() != DriverLocal {
		t.Errorf("driver = %q, want local", store.Driver())
	}

	cfg.Storage.Driver = "nope"
	if _, err := New(context.Background(), &cfg.Storage); err == nil {
		t.Error("New(unknown driver) = nil error")
	}
}
