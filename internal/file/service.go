package file

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/blobx"
)

// Options configures the file service. The allowlists are normalized by the
// config loader (lowercase, leading dot) and are copied into sets here.
type Options struct {
	// MaxUploadBytes caps one file.
	MaxUploadBytes int64
	// AllowedMIME is the allowlist of sniffed content types.
	AllowedMIME []string
	// AllowedExt is the allowlist of lowercase extensions with a leading dot.
	AllowedExt []string
	// InlineMIME lists types that may be rendered inline. Everything else is
	// forced to Content-Disposition: attachment.
	InlineMIME []string
	// Bucket is recorded on each row (S3 bucket; empty for local).
	Bucket string
	// AssetBaseURL is the public origin of long-lived asset links (empty
	// disables sharing: HTTP would return 500 rather than a dead link).
	AssetBaseURL string
	// AssetCacheTTL is the browser/CDN cache lifetime for /a/{token} responses;
	// it is also the worst-case revocation delay.
	AssetCacheTTL time.Duration
	// PublicTokenBytes is the raw entropy of an asset token (default 32).
	PublicTokenBytes int
	// UploadDailyFiles/UploadDailyBytes cap one user's rolling 24h upload
	// budget; 0 disables the cap.
	UploadDailyFiles int
	UploadDailyBytes int64
	// Now is injectable for tests.
	Now func() time.Time
}

// Service implements the file use-cases. It depends on Repository (metadata)
// and blobx.Store (bytes), so both can be faked in unit tests.
type Service struct {
	repo  Repository
	store blobx.Store
	opts  Options

	allowedMIME map[string]struct{}
	allowedExt  map[string]struct{}
	inlineMIME  map[string]struct{}
}

// NewService creates a Service.
func NewService(repo Repository, store blobx.Store, opts *Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts == nil {
		opts = &Options{}
	}
	if opts.AssetCacheTTL <= 0 {
		opts.AssetCacheTTL = 24 * time.Hour
	}
	if opts.PublicTokenBytes < 16 || opts.PublicTokenBytes > 64 {
		opts.PublicTokenBytes = 32
	}
	return &Service{
		repo:        repo,
		store:       store,
		opts:        *opts,
		allowedMIME: toSet(opts.AllowedMIME),
		allowedExt:  toSet(opts.AllowedExt),
		inlineMIME:  toSet(opts.InlineMIME),
	}
}

// UploadInput is one multipart file.
type UploadInput struct {
	// Filename is the client-provided name; it is cleaned, never used as a path.
	Filename string
	// Size is the declared part size (the service still counts real bytes).
	Size int64
	// Body streams the content.
	Body io.Reader
	// BizType groups files for the UI/quotas ("avatar", "attachment", ...).
	BizType string
	// Visibility defaults to private when nil.
	Visibility *Visibility
}

// ListQuery is the caller-facing listing query.
type ListQuery struct {
	Page     int
	PageSize int
	BizType  string
	Keyword  string
}

// URLResult is a signed download URL.
type URLResult struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Upload validates, streams and records one file. On a metadata failure the
// blob is deleted again so a failed upload never leaves an unreferenced object.
func (s *Service) Upload(ctx context.Context, actor authkit.Principal, in UploadInput) (*File, error) {
	if actor.UserID == 0 {
		return nil, apperr.New(apperr.CodeUnauthorized, "authentication required")
	}
	name, err := cleanFilename(in.Filename)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(path.Ext(name))
	if _, ok := s.allowedExt[ext]; !ok {
		return nil, apperr.New(apperr.CodeInvalidArgument, "file extension is not allowed")
	}
	bizType, err := normalizeBizType(in.BizType)
	if err != nil {
		return nil, err
	}
	if in.Size <= 0 {
		return nil, apperr.New(apperr.CodeInvalidArgument, "empty file")
	}
	if in.Size > s.opts.MaxUploadBytes {
		return nil, apperr.New(apperr.CodePayloadTooLarge, fmt.Sprintf("file exceeds the %d byte limit", s.opts.MaxUploadBytes))
	}
	if err := s.checkUploadQuota(ctx, actor.UserID); err != nil {
		return nil, err
	}
	visibility := VisibilityPrivate
	if in.Visibility != nil {
		if !in.Visibility.Valid() {
			return nil, apperr.New(apperr.CodeInvalidArgument, "invalid visibility")
		}
		visibility = *in.Visibility
	}

	reader := bufio.NewReader(in.Body)
	head, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, "read upload failed", err)
	}
	contentType, err := sniffContentType(head)
	if err != nil {
		return nil, err
	}

	now := s.opts.Now()
	key := fmt.Sprintf("file/%s/%s%s", now.Format("200601"), uuid.NewString(), ext)

	hasher := sha256.New()
	payload := io.TeeReader(reader, hasher)
	info, err := s.store.Put(ctx, key, payload, in.Size, contentType)
	if err != nil {
		return nil, uploadError(err)
	}

	ownerID := actor.UserID
	record := &File{
		Storage:      s.store.Driver(),
		Bucket:       s.opts.Bucket,
		ObjectKey:    key,
		OriginalName: name,
		Ext:          ext,
		MIMEType:     contentType,
		SizeBytes:    info.Size,
		SHA256:       hex.EncodeToString(hasher.Sum(nil)),
		BizType:      bizType,
		Visibility:   visibility,
		Status:       StatusReady,
		OwnerID:      &ownerID,
	}
	if err := s.repo.Create(ctx, record); err != nil {
		// Best-effort rollback: an orphan blob is worse than a missing row
		// (the janitor can only clean rows it knows about).
		_ = s.store.Delete(ctx, key)
		return nil, err
	}
	observeUpload(record.MIMEType, record.SizeBytes, "ok")
	return record, nil
}

// Get returns a file if the caller may read it.
func (s *Service) Get(ctx context.Context, actor authkit.Principal, id uint64) (*File, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canRead(actor, record) {
		// 403 (not 404) is deliberate for existing-but-forbidden: the list
		// endpoint already tells members that other owners' ids exist.
		return nil, apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	return record, nil
}

// List returns a page of files. Members only ever see their own; admins may
// filter by owner through the repository (no owner filter = all).
func (s *Service) List(ctx context.Context, actor authkit.Principal, q ListQuery) ([]File, int64, error) {
	page, pageSize := normalizePagination(q.Page, q.PageSize)
	filter := ListFilter{
		BizType:  strings.TrimSpace(q.BizType),
		Keyword:  strings.TrimSpace(q.Keyword),
		Page:     page,
		PageSize: pageSize,
	}
	if !actor.IsAdmin() {
		ownerID := actor.UserID
		filter.OwnerID = &ownerID
	}
	return s.repo.List(ctx, filter)
}

// URL returns a signed download URL. Inline rendering is only allowed for the
// configured inline MIME set; everything else is forced to attachment so an
// uploaded HTML/SVG can never execute on the API origin.
func (s *Service) URL(ctx context.Context, actor authkit.Principal, id uint64, disposition string, ttl time.Duration) (URLResult, error) {
	record, err := s.Get(ctx, actor, id)
	if err != nil {
		return URLResult{}, err
	}
	return s.signedURLFor(ctx, record, disposition, ttl)
}

// UpdateVisibility lets the owner (or an admin) share a file with every
// authenticated member.
func (s *Service) UpdateVisibility(ctx context.Context, actor authkit.Principal, id uint64, visibility Visibility) (*File, error) {
	if !visibility.Valid() {
		return nil, apperr.New(apperr.CodeInvalidArgument, "invalid visibility")
	}
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canWrite(actor, record) {
		return nil, apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	if err := s.repo.UpdateVisibility(ctx, id, visibility); err != nil {
		return nil, err
	}
	// Switching back to private must also kill the anonymous links, otherwise
	// "private" would still be readable through an already-issued asset URL.
	if visibility == VisibilityPrivate {
		if err := s.repo.RevokeShares(ctx, id, "", s.opts.Now()); err != nil {
			return nil, err
		}
	}
	record.Visibility = visibility
	return record, nil
}

// Delete soft-deletes the row and marks it for blob purge. Members may delete
// their own files; admins may delete anyone's.
func (s *Service) Delete(ctx context.Context, actor authkit.Principal, id uint64) error {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !canWrite(actor, record) {
		return apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	// Revoke public links first: a soft-deleted row already answers 404, but an
	// explicit revocation keeps the intent visible in the audit trail.
	if err := s.repo.RevokeShares(ctx, id, "", s.opts.Now()); err != nil {
		return err
	}
	return s.repo.MarkDeleting(ctx, id, s.opts.Now())
}

// PurgeDeleting removes the blobs of rows marked deleting before the cutoff and
// then hard-deletes the rows. It is idempotent: a missing blob still lets the
// row go away, so a retried tick never accumulates unmovable rows.
func (s *Service) PurgeDeleting(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.repo.ListByStatusBefore(ctx, StatusDeleting, before, limit)
	if err != nil {
		return 0, err
	}
	var purged int64
	var lastErr error
	for i := range rows {
		started := time.Now()
		if err := s.store.Delete(ctx, rows[i].ObjectKey); err != nil && !errors.Is(err, blobx.ErrNotFound) {
			observeStorageOp(s.store.Driver(), "delete", "error", time.Since(started))
			lastErr = err
			continue // keep the row: the next tick retries
		}
		observeStorageOp(s.store.Driver(), "delete", "ok", time.Since(started))
		if err := s.repo.HardDelete(ctx, rows[i].ID); err != nil {
			lastErr = err
			continue
		}
		purged++
	}
	return purged, lastErr
}

// MarkOrphansDeleting moves abandoned uploads (ready, created before the
// cutoff, no active link) into the deleting state so PurgeDeleting can clean
// them. The cutoff is supplied by the scheduler (now - orphan_ttl).
func (s *Service) MarkOrphansDeleting(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.repo.ListOrphans(ctx, before, limit)
	if err != nil {
		return 0, err
	}
	var marked int64
	var lastErr error
	for i := range rows {
		if err := s.repo.MarkDeleting(ctx, rows[i].ID, s.opts.Now()); err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				continue
			}
			lastErr = err
			continue
		}
		marked++
	}
	return marked, lastErr
}

// FindForDownload loads a ready file by id without an actor check. It is only
// used by the signed local-download route, where the HMAC signature is the
// credential; never expose it through an authenticated API handler.
func (s *Service) FindForDownload(ctx context.Context, id uint64) (*File, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusReady {
		return nil, apperr.ErrNotFound
	}
	return record, nil
}

// Open streams a stored object. The returned reader may implement io.Seeker
// (local driver), which the HTTP layer uses for Range requests.
func (s *Service) Open(ctx context.Context, record *File) (io.ReadCloser, blobx.ObjectInfo, error) {
	started := time.Now()
	reader, info, err := s.store.Open(ctx, record.ObjectKey)
	if err != nil {
		observeStorageOp(s.store.Driver(), "open", "error", time.Since(started))
		if errors.Is(err, blobx.ErrNotFound) {
			return nil, blobx.ObjectInfo{}, apperr.ErrNotFound
		}
		return nil, blobx.ObjectInfo{}, apperr.Wrap(apperr.CodeInternal, "open file failed", err)
	}
	observeStorageOp(s.store.Driver(), "open", "ok", time.Since(started))
	return reader, info, nil
}

// PreviewTextLimit caps the text preview response (bytes read from storage).
const PreviewTextLimit = 256 << 10

// PreviewText returns a bounded plain-text preview for text/* files. Ownership
// and visibility follow the normal Get rules.
func (s *Service) PreviewText(ctx context.Context, actor authkit.Principal, id uint64) (TextPreviewResponse, error) {
	record, err := s.Get(ctx, actor, id)
	if err != nil {
		return TextPreviewResponse{}, err
	}
	if !strings.HasPrefix(record.MIMEType, "text/") {
		return TextPreviewResponse{}, apperr.New(apperr.CodeInvalidArgument, "file is not a text file")
	}
	reader, _, err := s.Open(ctx, record)
	if err != nil {
		return TextPreviewResponse{}, err
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(io.LimitReader(reader, PreviewTextLimit+1))
	if err != nil {
		return TextPreviewResponse{}, apperr.Wrap(apperr.CodeInternal, "read preview failed", err)
	}
	truncated := len(data) > PreviewTextLimit
	if truncated {
		data = data[:PreviewTextLimit]
	}
	return TextPreviewResponse{
		Content:   strings.ToValidUTF8(string(data), "\uFFFD"),
		Truncated: truncated,
		SizeBytes: record.SizeBytes,
	}, nil
}

// signedURLFor builds the URL for an already-authorized row.
func (s *Service) signedURLFor(ctx context.Context, record *File, disposition string, ttl time.Duration) (URLResult, error) {
	disposition = strings.ToLower(strings.TrimSpace(disposition))
	switch disposition {
	case "":
		disposition = "attachment"
	case "inline", "attachment":
	default:
		return URLResult{}, apperr.New(apperr.CodeInvalidArgument, "disposition must be inline or attachment")
	}
	if disposition == "inline" {
		if _, ok := s.inlineMIME[record.MIMEType]; !ok {
			disposition = "attachment"
		}
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if ttl > time.Hour {
		ttl = time.Hour
	}
	started := time.Now()
	url, expires, err := s.store.URL(ctx, record.ObjectKey, blobx.SignedURLOptions{
		Expiry:      ttl,
		Disposition: disposition,
		Filename:    record.OriginalName,
		ContentType: record.MIMEType,
		PathID:      fmt.Sprintf("%d/%s", record.ID, disposition),
	})
	if err != nil {
		observeStorageOp(s.store.Driver(), "url", "error", time.Since(started))
		return URLResult{}, apperr.Wrap(apperr.CodeInternal, "build download URL failed", err)
	}
	observeStorageOp(s.store.Driver(), "url", "ok", time.Since(started))
	return URLResult{URL: url, ExpiresAt: expires}, nil
}

// canRead reports whether actor may read f.
func canRead(actor authkit.Principal, f *File) bool {
	return actor.IsAdmin() || f.IsOwner(actor.UserID) || f.Visibility == VisibilityPublic
}

// canWrite reports whether actor may mutate/delete f.
func canWrite(actor authkit.Principal, f *File) bool {
	return actor.IsAdmin() || f.IsOwner(actor.UserID)
}

// cleanFilename strips directories, control characters and overlong names so a
// client filename can never influence the storage path or the response headers.
func cleanFilename(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSpace(path.Base(name))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '"' || r == ':' || r == '*' || r == '?' || r == '<' || r == '>' || r == '|' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	name = strings.Trim(name, ".")
	if name == "" {
		return "", apperr.New(apperr.CodeInvalidArgument, "file name is required")
	}
	if len(name) > 255 {
		// Truncate on a rune boundary so the name stays valid UTF-8.
		cut := 255
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	return name, nil
}

// sniffContentType derives the content type from the bytes, never from the
// client header, and enforces the allowlist.
func sniffContentType(head []byte) (string, error) {
	mediaType, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil || mediaType == "" {
		return "", apperr.New(apperr.CodeInvalidArgument, "cannot determine the file type")
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/octet-stream" || mediaType == "text/html" || mediaType == "image/svg+xml" {
		// Explicitly refused: octet-stream cannot be checked against an
		// allowlist, and HTML/SVG execute if a browser renders them inline.
		return "", apperr.New(apperr.CodeInvalidArgument, "file type is not allowed")
	}
	return mediaType, nil
}

// uploadError maps storage failures to business errors.
func uploadError(err error) error {
	switch {
	case errors.Is(err, blobx.ErrTooLarge):
		return apperr.Wrap(apperr.CodePayloadTooLarge, "file exceeds the size limit", err)
	case errors.Is(err, blobx.ErrSizeMismatch):
		return apperr.Wrap(apperr.CodeInvalidArgument, "uploaded byte count does not match the declared size", err)
	default:
		observeUpload("", 0, "error")
		return apperr.Wrap(apperr.CodeInternal, "store file failed", err)
	}
}

// normalizeBizType keeps biz_type a small machine-readable token.
func normalizeBizType(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if len(value) > 32 {
		return "", apperr.New(apperr.CodeInvalidArgument, "biz_type must be at most 32 characters")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return "", apperr.New(apperr.CodeInvalidArgument, "biz_type may only contain a-z, 0-9, _, - and .")
	}
	return value, nil
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// normalizePagination clamps caller-supplied paging into a safe range.
func normalizePagination(page, pageSize int) (normalizedPage, normalizedPageSize int) {
	normalizedPage, normalizedPageSize = page, pageSize
	if normalizedPage < 1 {
		normalizedPage = 1
	}
	if normalizedPageSize < 1 {
		normalizedPageSize = 20
	}
	if normalizedPageSize > 100 {
		normalizedPageSize = 100
	}
	return normalizedPage, normalizedPageSize
}
