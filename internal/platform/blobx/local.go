package blobx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

// Local stores objects under a filesystem root and serves them through the app
// with HMAC-signed URLs. The root is opened as an *os.Root, so even a bug in
// key generation cannot escape it via "..", absolute paths or a symlink.
type Local struct {
	root    *os.Root
	baseURL string
	secret  []byte
}

// NewLocal opens (and creates) root and returns the filesystem driver. baseURL
// is the absolute public origin used for signed download URLs; secret signs
// them and must be non-empty.
func NewLocal(root, baseURL string, secret []byte) (*Local, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("blobx: create local root: %w", err)
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("blobx: open local root: %w", err)
	}
	if len(secret) == 0 {
		_ = opened.Close()
		return nil, fmt.Errorf("blobx: local signing secret must not be empty")
	}
	if baseURL == "" {
		_ = opened.Close()
		return nil, fmt.Errorf("blobx: local base URL must not be empty")
	}
	return &Local{root: opened, baseURL: baseURL, secret: append([]byte(nil), secret...)}, nil
}

// Driver implements Store.
func (l *Local) Driver() string { return DriverLocal }

// Close releases the root handle.
func (l *Local) Close() error { return l.root.Close() }

// Root reports the absolute filesystem root (diagnostics/tests only).
func (l *Local) Root() string { return l.root.Name() }

// Put implements Store. It writes to a temporary file inside the root and
// renames it into place, so a failed/interrupted upload never leaves a partial
// object behind under its final key.
func (l *Local) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	if dir := path.Dir(key); dir != "." {
		if err := l.root.MkdirAll(dir, 0o750); err != nil {
			return ObjectInfo{}, fmt.Errorf("blobx: create object directory: %w", err)
		}
	}
	tmpDir := ".tmp"
	if err := l.root.MkdirAll(tmpDir, 0o750); err != nil {
		return ObjectInfo{}, fmt.Errorf("blobx: create temp directory: %w", err)
	}
	tmpName := path.Join(tmpDir, strconv.FormatInt(time.Now().UnixNano(), 36)+"-"+filepath.Base(key))
	file, err := l.root.Create(tmpName)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("blobx: create temp object: %w", err)
	}
	written, copyErr := copyLimited(file, body, size)
	closeErr := file.Close()
	if copyErr != nil {
		_ = l.root.Remove(tmpName)
		return ObjectInfo{}, copyErr
	}
	if closeErr != nil {
		_ = l.root.Remove(tmpName)
		return ObjectInfo{}, fmt.Errorf("blobx: close temp object: %w", closeErr)
	}
	if size >= 0 && written != size {
		_ = l.root.Remove(tmpName)
		return ObjectInfo{}, fmt.Errorf("%w: wrote %d of %d bytes", ErrSizeMismatch, written, size)
	}
	if err := l.root.Rename(tmpName, key); err != nil {
		_ = l.root.Remove(tmpName)
		return ObjectInfo{}, fmt.Errorf("blobx: commit object: %w", err)
	}
	info, err := l.Stat(ctx, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info.ContentType = contentType
	return info, nil
}

// Open implements Store. The returned reader is an *os.File, so the HTTP layer
// can use http.ServeContent (range requests) when it wants to.
func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := l.root.Open(key)
	if err != nil {
		return nil, ObjectInfo{}, notFoundOr(err, "open object")
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, ObjectInfo{}, fmt.Errorf("blobx: stat object: %w", err)
	}
	return file, ObjectInfo{Key: key, Size: stat.Size(), ModTime: stat.ModTime()}, nil
}

// Stat implements Store.
func (l *Local) Stat(_ context.Context, key string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	stat, err := l.root.Stat(key)
	if err != nil {
		return ObjectInfo{}, notFoundOr(err, "stat object")
	}
	return ObjectInfo{Key: key, Size: stat.Size(), ModTime: stat.ModTime()}, nil
}

// Delete implements Store. A missing object is not an error (idempotent purge).
func (l *Local) Delete(_ context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := l.root.Remove(key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blobx: delete object: %w", err)
	}
	return nil
}

// URL implements Store. The URL points at the app's own download route
// (GET /files/{PathID}/{exp}/{sig}); the signature covers the storage key, so
// the route does not have to trust anything in the path except the file id.
func (l *Local) URL(_ context.Context, key string, opt SignedURLOptions) (string, time.Time, error) {
	if err := ValidateKey(key); err != nil {
		return "", time.Time{}, err
	}
	if opt.PathID == "" {
		return "", time.Time{}, fmt.Errorf("blobx: local signed URL needs a PathID")
	}
	expiry := clampExpiry(opt.Expiry, 10*time.Minute)
	expires := time.Now().Add(expiry)
	sig := SignLocalURL(l.secret, key, expires.Unix(), opt.Disposition)
	url := fmt.Sprintf("%s/files/%s/%d/%s", l.baseURL, opt.PathID, expires.Unix(), sig)
	return url, expires, nil
}

// SignLocalURL returns the hex HMAC-SHA256 over key, expiry and disposition.
// It is exported so the HTTP download handler verifies with the exact same
// derivation the driver signs with.
func SignLocalURL(secret []byte, key string, expires int64, disposition string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(key))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(strconv.FormatInt(expires, 10)))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(disposition))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyLocalURL checks a signature produced by SignLocalURL. It returns
// ErrNotFound (not "forbidden") for a bad signature so an attacker cannot use
// the response to probe which file ids exist.
func VerifyLocalURL(secret []byte, key string, expires int64, disposition, sig string, now time.Time) error {
	if now.Unix() > expires {
		return fmt.Errorf("%w: signed URL expired", ErrNotFound)
	}
	expected := SignLocalURL(secret, key, expires, disposition)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("%w: bad signature", ErrNotFound)
	}
	return nil
}

// copyLimited copies at most size+1 bytes (size < 0 means "no declared limit")
// so a stream that is longer than promised is detected instead of trusted.
func copyLimited(dst io.Writer, src io.Reader, size int64) (int64, error) {
	if size < 0 {
		return io.Copy(dst, src)
	}
	written, err := io.Copy(dst, io.LimitReader(src, size+1))
	if err != nil {
		return written, fmt.Errorf("blobx: write object: %w", err)
	}
	if written > size {
		return written, ErrTooLarge
	}
	return written, nil
}

// notFoundOr maps os.ErrNotExist to the blobx sentinel.
func notFoundOr(err error, action string) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, action)
	}
	return fmt.Errorf("blobx: %s: %w", action, err)
}
