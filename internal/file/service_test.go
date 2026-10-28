package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/blobx"
)

// ---- fakes ---------------------------------------------------------------

type fakeStore struct {
	objects   map[string][]byte
	lastPut   blobx.SignedURLOptions
	lastURL   string
	deleted   []string
	putErr    error
	deleteErr error
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (s *fakeStore) Driver() string { return blobx.DriverLocal }

func (s *fakeStore) Put(_ context.Context, key string, body io.Reader, size int64, contentType string) (blobx.ObjectInfo, error) {
	if s.putErr != nil {
		return blobx.ObjectInfo{}, s.putErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return blobx.ObjectInfo{}, err
	}
	if int64(len(data)) > size {
		return blobx.ObjectInfo{}, blobx.ErrTooLarge
	}
	if int64(len(data)) != size {
		return blobx.ObjectInfo{}, blobx.ErrSizeMismatch
	}
	s.objects[key] = data
	return blobx.ObjectInfo{Key: key, Size: size, ContentType: contentType}, nil
}

func (s *fakeStore) Open(_ context.Context, key string) (io.ReadCloser, blobx.ObjectInfo, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, blobx.ObjectInfo{}, blobx.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), blobx.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}

func (s *fakeStore) Stat(_ context.Context, key string) (blobx.ObjectInfo, error) {
	data, ok := s.objects[key]
	if !ok {
		return blobx.ObjectInfo{}, blobx.ErrNotFound
	}
	return blobx.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}

func (s *fakeStore) Delete(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}

func (s *fakeStore) URL(_ context.Context, key string, opt blobx.SignedURLOptions) (string, time.Time, error) {
	s.lastPut = opt
	s.lastURL = "signed:" + key
	return s.lastURL, time.Now().Add(opt.Expiry), nil
}

type fakeRepo struct {
	files     map[uint64]*File
	links     []Link
	shares    []Share
	nextID    uint64
	linkID    uint64
	shareID   uint64
	createErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{files: map[uint64]*File{}} }

func (r *fakeRepo) Create(_ context.Context, f *File) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.nextID++
	f.ID = r.nextID
	f.CreatedAt = time.Now()
	f.UpdatedAt = f.CreatedAt
	copied := *f
	r.files[f.ID] = &copied
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id uint64) (*File, error) {
	f, ok := r.files[id]
	if !ok || f.DeletedAt != nil {
		return nil, apperr.ErrNotFound
	}
	copied := *f
	return &copied, nil
}

func (r *fakeRepo) List(_ context.Context, filter ListFilter) ([]File, int64, error) {
	var out []File
	for _, f := range r.files {
		if f.DeletedAt != nil {
			continue
		}
		if filter.OwnerID != nil && (f.OwnerID == nil || *f.OwnerID != *filter.OwnerID) {
			continue
		}
		if filter.BizType != "" && f.BizType != filter.BizType {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(f.OriginalName), strings.ToLower(filter.Keyword)) {
			continue
		}
		out = append(out, *f)
	}
	total := int64(len(out))
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(out) {
		return nil, total, nil
	}
	end := start + filter.PageSize
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], total, nil
}

func (r *fakeRepo) UpdateVisibility(_ context.Context, id uint64, visibility Visibility) error {
	f, ok := r.files[id]
	if !ok {
		return apperr.ErrNotFound
	}
	f.Visibility = visibility
	return nil
}

func (r *fakeRepo) MarkDeleting(_ context.Context, id uint64, at time.Time) error {
	f, ok := r.files[id]
	if !ok || f.DeletedAt != nil {
		return apperr.ErrNotFound
	}
	f.Status = StatusDeleting
	f.DeletedAt = &at
	return nil
}

func (r *fakeRepo) HardDelete(_ context.Context, id uint64) error {
	delete(r.files, id)
	return nil
}

func (r *fakeRepo) ListByStatusBefore(_ context.Context, status Status, before time.Time, limit int) ([]File, error) {
	var out []File
	for _, f := range r.files {
		if f.Status == status && f.CreatedAt.Before(before) && len(out) < limit {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListOrphans(_ context.Context, before time.Time, limit int) ([]File, error) {
	var out []File
	for _, f := range r.files {
		if f.Status != StatusReady || f.DeletedAt != nil || !f.CreatedAt.Before(before) || len(out) >= limit {
			continue
		}
		linked := false
		for _, l := range r.links {
			if l.FileID == f.ID && l.DeletedAt == nil {
				linked = true
			}
		}
		if !linked {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (r *fakeRepo) CreateLink(_ context.Context, link *Link) error {
	for _, existing := range r.links {
		if existing.DeletedAt == nil && existing.FileID == link.FileID &&
			existing.OwnerType == link.OwnerType && existing.OwnerID == link.OwnerID && existing.Slot == link.Slot {
			return nil // idempotent, like the unique partial index
		}
	}
	r.linkID++
	link.ID = r.linkID
	r.links = append(r.links, *link)
	return nil
}

func (r *fakeRepo) SoftDeleteLinks(_ context.Context, fileID uint64, ownerType string, ownerID uint64, slot string, at time.Time) error {
	for i := range r.links {
		link := &r.links[i]
		if link.FileID == fileID && link.OwnerType == ownerType && link.OwnerID == ownerID && (slot == "" || link.Slot == slot) && link.DeletedAt == nil {
			link.DeletedAt = &at
		}
	}
	return nil
}

func (r *fakeRepo) SoftDeleteLinksByOwner(_ context.Context, ownerType string, ownerID uint64, at time.Time) error {
	for i := range r.links {
		link := &r.links[i]
		if link.OwnerType == ownerType && link.OwnerID == ownerID && link.DeletedAt == nil {
			link.DeletedAt = &at
		}
	}
	return nil
}

func (r *fakeRepo) ListFilesByOwner(_ context.Context, ownerType string, ownerID uint64) ([]File, error) {
	var out []File
	for _, link := range r.links {
		if link.OwnerType != ownerType || link.OwnerID != ownerID || link.DeletedAt != nil {
			continue
		}
		if f, ok := r.files[link.FileID]; ok && f.DeletedAt == nil {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (r *fakeRepo) CreateShare(_ context.Context, share *Share) error {
	for _, existing := range r.shares {
		if existing.RevokedAt == nil && existing.FileID == share.FileID && existing.Purpose == share.Purpose {
			return apperr.New(apperr.CodeConflict, "share already exists")
		}
	}
	r.shareID++
	share.ID = r.shareID
	r.shares = append(r.shares, *share)
	return nil
}

func (r *fakeRepo) FindActiveShareByToken(_ context.Context, token string, now time.Time) (*Share, error) {
	for _, share := range r.shares {
		if share.Token != token || share.RevokedAt != nil || (share.ExpiresAt != nil && !share.ExpiresAt.After(now)) {
			continue
		}
		copied := share
		return &copied, nil
	}
	return nil, apperr.ErrNotFound
}

func (r *fakeRepo) FindActiveShareByPurpose(_ context.Context, fileID uint64, purpose string, now time.Time) (*Share, error) {
	for _, share := range r.shares {
		if share.FileID != fileID || share.Purpose != purpose || share.RevokedAt != nil {
			continue
		}
		if share.ExpiresAt != nil && !share.ExpiresAt.After(now) {
			continue
		}
		copied := share
		return &copied, nil
	}
	return nil, apperr.ErrNotFound
}

func (r *fakeRepo) RevokeShares(_ context.Context, fileID uint64, purpose string, at time.Time) error {
	for i := range r.shares {
		share := &r.shares[i]
		if share.FileID != fileID || share.RevokedAt != nil {
			continue
		}
		if purpose != "" && share.Purpose != purpose {
			continue
		}
		share.RevokedAt = &at
	}
	return nil
}

func (r *fakeRepo) UsageSince(_ context.Context, ownerID uint64, since time.Time) (fileCount, byteCount int64, err error) {
	for _, f := range r.files {
		if f.OwnerID == nil || *f.OwnerID != ownerID || f.DeletedAt != nil || f.CreatedAt.Before(since) {
			continue
		}
		fileCount++
		byteCount += f.SizeBytes
	}
	return fileCount, byteCount, nil
}

func (r *fakeRepo) CountActiveLinks(_ context.Context, fileID uint64) (int64, error) {
	var count int64
	for _, link := range r.links {
		if link.FileID == fileID && link.DeletedAt == nil {
			count++
		}
	}
	return count, nil
}

// codeIs asserts on the business code of an error (apperr.Is matches sentinel
// errors; tests usually care about the code).
func codeIs(err error, code apperr.Code) bool {
	var target *apperr.Error
	return errors.As(err, &target) && target.Code == code
}

// ---- fixtures ------------------------------------------------------------

var (
	memberActor = authkit.Principal{UserID: 7, Role: authkit.RoleMember}
	otherActor  = authkit.Principal{UserID: 8, Role: authkit.RoleMember}
	adminActor  = authkit.Principal{UserID: 1, Role: authkit.RoleAdmin}
	pngBytes    = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))
)

func newTestService(t *testing.T) (*Service, *fakeRepo, *fakeStore) {
	t.Helper()
	repo := newFakeRepo()
	store := newFakeStore()
	svc := NewService(repo, store, &Options{
		MaxUploadBytes: 1 << 20,
		AllowedMIME:    []string{"image/png", "text/plain", "application/pdf"},
		AllowedExt:     []string{".png", ".txt", ".pdf"},
		InlineMIME:     []string{"image/png", "application/pdf"},
		Bucket:         "test-bucket",
		AssetBaseURL:   "http://assets.test",
	})
	return svc, repo, store
}

func uploadPNG(t *testing.T, svc *Service, actor authkit.Principal) *File {
	t.Helper()
	record, err := svc.Upload(context.Background(), actor, UploadInput{
		Filename: "logo.png",
		Size:     int64(len(pngBytes)),
		Body:     bytes.NewReader(pngBytes),
		BizType:  "avatar",
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	return record
}

// ---- tests ---------------------------------------------------------------

func TestUploadStoresMetadataAndBytes(t *testing.T) {
	svc, repo, store := newTestService(t)
	record := uploadPNG(t, svc, memberActor)

	if record.MIMEType != "image/png" || record.Ext != ".png" || record.OriginalName != "logo.png" {
		t.Errorf("record = %+v", record)
	}
	if record.SizeBytes != int64(len(pngBytes)) || record.Status != StatusReady || record.Visibility != VisibilityPrivate {
		t.Errorf("metadata mismatch: %+v", record)
	}
	sum := sha256.Sum256(pngBytes)
	if record.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q, want the content digest", record.SHA256)
	}
	if record.OwnerID == nil || *record.OwnerID != memberActor.UserID {
		t.Errorf("owner = %v, want %d", record.OwnerID, memberActor.UserID)
	}
	if !strings.HasPrefix(record.ObjectKey, "file/") || !strings.HasSuffix(record.ObjectKey, ".png") {
		t.Errorf("object key = %q", record.ObjectKey)
	}
	if _, ok := store.objects[record.ObjectKey]; !ok {
		t.Errorf("bytes were not stored under %q", record.ObjectKey)
	}
	if record.Storage != blobx.DriverLocal || record.Bucket != "test-bucket" {
		t.Errorf("storage locator = %q/%q", record.Storage, record.Bucket)
	}
	if len(repo.files) != 1 {
		t.Errorf("rows = %d, want 1", len(repo.files))
	}
}

func TestUploadRejectsBadInput(t *testing.T) {
	svc, _, store := newTestService(t)
	cases := []struct {
		name string
		in   UploadInput
		want apperr.Code
	}{
		{"bad extension", UploadInput{Filename: "evil.exe", Size: 4, Body: strings.NewReader("MZ..")}, apperr.CodeInvalidArgument},
		{"html content", UploadInput{Filename: "note.txt", Size: int64(len("<html><body>x</body></html>")), Body: strings.NewReader("<html><body>x</body></html>")}, apperr.CodeInvalidArgument},
		{"empty", UploadInput{Filename: "a.png", Size: 0, Body: strings.NewReader("")}, apperr.CodeInvalidArgument},
		{"too large", UploadInput{Filename: "a.png", Size: 2 << 20, Body: bytes.NewReader(pngBytes)}, apperr.CodePayloadTooLarge},
		{"path traversal name", UploadInput{Filename: "../../etc/passwd", Size: 1, Body: strings.NewReader("x")}, apperr.CodeInvalidArgument},
		{"bad biz type", UploadInput{Filename: "a.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes), BizType: "not a token!"}, apperr.CodeInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Upload(context.Background(), memberActor, tc.in)
			if !codeIs(err, tc.want) {
				t.Fatalf("err = %v, want code %v", err, tc.want)
			}
		})
	}
	if len(store.objects) != 0 {
		t.Errorf("rejected uploads left %d objects behind", len(store.objects))
	}

	visibility := Visibility(9)
	if _, err := svc.Upload(context.Background(), memberActor, UploadInput{
		Filename: "a.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes), Visibility: &visibility,
	}); !codeIs(err, apperr.CodeInvalidArgument) {
		t.Errorf("invalid visibility error = %v", err)
	}
	if _, err := svc.Upload(context.Background(), authkit.Principal{}, UploadInput{
		Filename: "a.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); !codeIs(err, apperr.CodeUnauthorized) {
		t.Errorf("anonymous upload error = %v", err)
	}
}

func TestUploadRollsBackBlobWhenMetadataFails(t *testing.T) {
	svc, repo, store := newTestService(t)
	repo.createErr = apperr.New(apperr.CodeDatabase, "boom")

	if _, err := svc.Upload(context.Background(), memberActor, UploadInput{
		Filename: "a.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); !codeIs(err, apperr.CodeDatabase) {
		t.Fatalf("err = %v, want database error", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("blob was not rolled back: %v", store.objects)
	}
}

func TestUploadMapsStoreFailures(t *testing.T) {
	svc, _, store := newTestService(t)
	store.putErr = blobx.ErrTooLarge
	if _, err := svc.Upload(context.Background(), memberActor, UploadInput{
		Filename: "a.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); !codeIs(err, apperr.CodePayloadTooLarge) {
		t.Errorf("too-large error = %v", err)
	}
	store.putErr = errors.New("disk on fire")
	if _, err := svc.Upload(context.Background(), memberActor, UploadInput{
		Filename: "a.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); !codeIs(err, apperr.CodeInternal) {
		t.Errorf("storage error = %v", err)
	}
}

func TestOwnershipRules(t *testing.T) {
	svc, _, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	if _, err := svc.Get(ctx, memberActor, record.ID); err != nil {
		t.Errorf("owner Get = %v", err)
	}
	if _, err := svc.Get(ctx, otherActor, record.ID); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("other member Get = %v, want forbidden", err)
	}
	if _, err := svc.Get(ctx, adminActor, record.ID); err != nil {
		t.Errorf("admin Get = %v", err)
	}

	// Public files are readable by every authenticated member.
	if _, err := svc.UpdateVisibility(ctx, memberActor, record.ID, VisibilityPublic); err != nil {
		t.Fatalf("UpdateVisibility: %v", err)
	}
	if _, err := svc.Get(ctx, otherActor, record.ID); err != nil {
		t.Errorf("other member Get(public) = %v", err)
	}
	// Readable does not mean writable: only the owner/admin may delete.
	if err := svc.Delete(ctx, otherActor, record.ID); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("other member Delete(public) = %v, want forbidden", err)
	}
	if _, err := svc.UpdateVisibility(ctx, otherActor, record.ID, VisibilityPrivate); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("other member UpdateVisibility = %v, want forbidden", err)
	}
}

func TestListScopesMembersToTheirOwnFiles(t *testing.T) {
	svc, _, _ := newTestService(t)
	own := uploadPNG(t, svc, memberActor)

	foreign, err := svc.Upload(context.Background(), otherActor, UploadInput{
		Filename: "other.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	})
	if err != nil {
		t.Fatalf("Upload(other): %v", err)
	}

	files, total, err := svc.List(context.Background(), memberActor, ListQuery{})
	if err != nil {
		t.Fatalf("List(member): %v", err)
	}
	if total != 1 || len(files) != 1 || files[0].ID != own.ID {
		t.Errorf("member list = %d/%d ids, want only own file %d", total, len(files), own.ID)
	}

	_, total, err = svc.List(context.Background(), adminActor, ListQuery{})
	if err != nil {
		t.Fatalf("List(admin): %v", err)
	}
	if total != 2 {
		t.Errorf("admin total = %d, want 2 (own=%d foreign=%d)", total, own.ID, foreign.ID)
	}

	byBiz, total, err := svc.List(context.Background(), adminActor, ListQuery{BizType: "avatar"})
	if err != nil {
		t.Fatalf("List(biz): %v", err)
	}
	if total != 1 || len(byBiz) != 1 || byBiz[0].BizType != "avatar" {
		t.Errorf("biz filter = %d rows", total)
	}
}

func TestURLDispositionAndTTL(t *testing.T) {
	svc, _, store := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	result, err := svc.URL(ctx, memberActor, record.ID, "inline", 30*time.Minute)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if result.URL != "signed:"+record.ObjectKey {
		t.Errorf("url = %q", result.URL)
	}
	if store.lastPut.Disposition != "inline" {
		t.Errorf("disposition = %q, want inline for image/png", store.lastPut.Disposition)
	}
	if store.lastPut.Filename != "logo.png" {
		t.Errorf("filename = %q, want the original name", store.lastPut.Filename)
	}

	// text/plain is not on the inline allowlist: it must be forced to attachment.
	textRecord, err := svc.Upload(ctx, memberActor, UploadInput{
		Filename: "note.txt", Size: int64(len("hello world")), Body: strings.NewReader("hello world"),
	})
	if err != nil {
		t.Fatalf("Upload(txt): %v", err)
	}
	if _, err := svc.URL(ctx, memberActor, textRecord.ID, "inline", 0); err != nil {
		t.Fatalf("URL(txt): %v", err)
	}
	if store.lastPut.Disposition != "attachment" {
		t.Errorf("text/plain disposition = %q, want attachment", store.lastPut.Disposition)
	}

	// ttl is clamped to one hour.
	if _, err := svc.URL(ctx, memberActor, record.ID, "attachment", 48*time.Hour); err != nil {
		t.Fatalf("URL(ttl): %v", err)
	}
	if store.lastPut.Expiry != time.Hour {
		t.Errorf("expiry = %v, want clamped 1h", store.lastPut.Expiry)
	}
	if _, err := svc.URL(ctx, memberActor, record.ID, "bogus", 0); !codeIs(err, apperr.CodeInvalidArgument) {
		t.Errorf("invalid disposition error = %v", err)
	}
	if _, err := svc.URL(ctx, otherActor, record.ID, "attachment", 0); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("foreign URL error = %v", err)
	}
}

func TestDeleteMarksDeletingAndPurgeRemovesBlob(t *testing.T) {
	svc, repo, store := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	if err := svc.Delete(ctx, otherActor, record.ID); !codeIs(err, apperr.CodeForbidden) {
		t.Fatalf("foreign delete = %v, want forbidden", err)
	}
	if err := svc.Delete(ctx, memberActor, record.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repo.files[record.ID].Status != StatusDeleting || repo.files[record.ID].DeletedAt == nil {
		t.Errorf("row after delete = %+v", repo.files[record.ID])
	}
	// Soft-deleted rows are invisible through the repository contract.
	if _, err := svc.Get(ctx, memberActor, record.ID); !codeIs(err, apperr.CodeNotFound) {
		t.Errorf("Get after delete = %v, want not found", err)
	}

	purged, err := svc.PurgeDeleting(ctx, time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatalf("PurgeDeleting: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}
	if len(repo.files) != 0 {
		t.Errorf("row was not hard-deleted: %+v", repo.files)
	}
	if len(store.objects) != 0 {
		t.Errorf("blob was not deleted: %v", store.objects)
	}
}

func TestPurgeDeletingKeepsRowWhenBlobDeleteFails(t *testing.T) {
	svc, repo, store := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()
	if err := svc.Delete(ctx, memberActor, record.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	store.deleteErr = errors.New("s3 down")

	purged, err := svc.PurgeDeleting(ctx, time.Now().Add(time.Second), 10)
	if err == nil {
		t.Fatal("PurgeDeleting = nil error, want the storage failure")
	}
	if purged != 0 {
		t.Errorf("purged = %d, want 0", purged)
	}
	if len(repo.files) != 1 {
		t.Errorf("row disappeared despite the failed blob delete")
	}
}

func TestMarkOrphansDeletingRespectsLinksAndCutoff(t *testing.T) {
	svc, repo, _ := newTestService(t)
	linked := uploadPNG(t, svc, memberActor)
	orphan := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	if _, err := svc.Attach(ctx, memberActor, AttachInput{
		Ref: Ref{OwnerType: "user.avatar", OwnerID: 7}, FileID: linked.ID, Slot: "avatar",
	}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	// Too new: nothing is old enough to be an orphan.
	if marked, err := svc.MarkOrphansDeleting(ctx, time.Now().Add(-time.Hour), 10); err != nil || marked != 0 {
		t.Fatalf("MarkOrphansDeleting(fresh) = %d, %v", marked, err)
	}
	marked, err := svc.MarkOrphansDeleting(ctx, time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatalf("MarkOrphansDeleting: %v", err)
	}
	if marked != 1 {
		t.Errorf("marked = %d, want 1 (only the unlinked file)", marked)
	}
	if repo.files[linked.ID].Status != StatusReady {
		t.Errorf("linked file was marked deleting")
	}
	if repo.files[orphan.ID].Status != StatusDeleting {
		t.Errorf("orphan file was not marked deleting")
	}
}

func TestBinderAttachDetachAndList(t *testing.T) {
	svc, repo, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()
	ref := Ref{OwnerType: "message.attachment", OwnerID: 99}

	if _, err := svc.Attach(ctx, otherActor, AttachInput{Ref: ref, FileID: record.ID}); !codeIs(err, apperr.CodeForbidden) {
		t.Fatalf("Attach(foreign) = %v, want forbidden", err)
	}
	if _, err := svc.Attach(ctx, memberActor, AttachInput{Ref: ref, FileID: record.ID}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// Idempotent: attaching again must not create a second active link.
	if _, err := svc.Attach(ctx, memberActor, AttachInput{Ref: ref, FileID: record.ID}); err != nil {
		t.Fatalf("Attach(again): %v", err)
	}
	if count, _ := repo.CountActiveLinks(ctx, record.ID); count != 1 {
		t.Errorf("active links = %d, want 1", count)
	}

	files, err := svc.ListByOwner(ctx, ref.OwnerType, ref.OwnerID)
	if err != nil || len(files) != 1 || files[0].ID != record.ID {
		t.Fatalf("ListByOwner = %+v, %v", files, err)
	}

	if err := svc.Detach(ctx, otherActor, AttachInput{Ref: ref, FileID: record.ID}); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("Detach(foreign) = %v, want forbidden", err)
	}
	if err := svc.Detach(ctx, memberActor, AttachInput{Ref: ref, FileID: record.ID}); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if files, _ := svc.ListByOwner(ctx, ref.OwnerType, ref.OwnerID); len(files) != 0 {
		t.Errorf("ListByOwner after detach = %d files", len(files))
	}

	// DeleteByOwner drops every remaining attachment of the resource.
	if _, err := svc.Attach(ctx, memberActor, AttachInput{Ref: ref, FileID: record.ID}); err != nil {
		t.Fatalf("Attach(resource): %v", err)
	}
	if err := svc.DeleteByOwner(ctx, ref.OwnerType, ref.OwnerID); err != nil {
		t.Fatalf("DeleteByOwner: %v", err)
	}
	if count, _ := repo.CountActiveLinks(ctx, record.ID); count != 0 {
		t.Errorf("active links after DeleteByOwner = %d", count)
	}

	// SignedURL is the trusted internal path: no actor required.
	if _, err := svc.SignedURL(ctx, record.ID, "attachment"); err != nil {
		t.Errorf("SignedURL: %v", err)
	}
	if _, err := svc.SignedURL(ctx, 9999, "attachment"); !codeIs(err, apperr.CodeNotFound) {
		t.Errorf("SignedURL(missing) = %v", err)
	}
}

func TestBinderValidation(t *testing.T) {
	svc, _, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	cases := []AttachInput{
		{Ref: Ref{OwnerType: "Bad Type", OwnerID: 1}, FileID: record.ID},
		{Ref: Ref{OwnerType: "user.avatar", OwnerID: 0}, FileID: record.ID},
		{Ref: Ref{OwnerType: "user.avatar", OwnerID: 1}, FileID: 0},
		{Ref: Ref{OwnerType: "user.avatar", OwnerID: 1}, FileID: record.ID, Slot: "not a slot"},
	}
	for _, in := range cases {
		if _, err := svc.Attach(ctx, memberActor, in); !codeIs(err, apperr.CodeInvalidArgument) {
			t.Errorf("Attach(%+v) = %v, want invalid argument", in, err)
		}
	}
	// A file that is being deleted cannot be attached.
	if err := svc.Delete(ctx, memberActor, record.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Attach(ctx, memberActor, AttachInput{Ref: Ref{OwnerType: "user.avatar", OwnerID: 1}, FileID: record.ID}); !codeIs(err, apperr.CodeNotFound) {
		t.Errorf("Attach(deleted) = %v, want not found", err)
	}
}

func TestCleanFilename(t *testing.T) {
	ok := map[string]string{
		"report.pdf":          "report.pdf",
		"../../etc/passwd":    "passwd",
		`C:\tmp\..\a\b.txt`:   "b.txt",
		"  spaced name.png  ": "spaced name.png",
		"bad:char*?.txt":      "bad_char__.txt",
	}
	for in, want := range ok {
		got, err := cleanFilename(in)
		if err != nil || got != want {
			t.Errorf("cleanFilename(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".", "..", "   "} {
		if _, err := cleanFilename(in); !codeIs(err, apperr.CodeInvalidArgument) {
			t.Errorf("cleanFilename(%q) = %v, want invalid argument", in, err)
		}
	}
}

// ---- public asset links (file_shares) ------------------------------------

func TestPublicURLIsIdempotentAndRevocable(t *testing.T) {
	svc, repo, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	first, err := svc.PublicURL(ctx, record.ID, "")
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	if first.Purpose != DefaultSharePurpose {
		t.Errorf("purpose = %q, want %q", first.Purpose, DefaultSharePurpose)
	}
	if !strings.HasPrefix(first.URL, "http://assets.test/a/") || !strings.HasSuffix(first.URL, "/logo.png") {
		t.Errorf("asset URL = %q", first.URL)
	}
	// Idempotent: same file+purpose must reuse the token so cached HTML stays valid.
	second, err := svc.PublicURL(ctx, record.ID, "")
	if err != nil {
		t.Fatalf("PublicURL again: %v", err)
	}
	if second.URL != first.URL {
		t.Errorf("URL changed between calls:\n %s\n %s", first.URL, second.URL)
	}
	if len(repo.shares) != 1 {
		t.Errorf("shares = %d, want 1", len(repo.shares))
	}

	// Different purpose = different token (independent revocation).
	og, err := svc.PublicURL(ctx, record.ID, "og-image")
	if err != nil {
		t.Fatalf("PublicURL(og): %v", err)
	}
	if og.URL == first.URL {
		t.Error("different purposes shared one token")
	}

	// Asset resolution works for active tokens.
	if _, err := svc.Asset(ctx, tokenOf(t, first.URL)); err != nil {
		t.Fatalf("Asset(active) = %v", err)
	}

	// Revoking one purpose leaves the other alive.
	if err := svc.RevokeSharesForFile(ctx, record.ID, DefaultSharePurpose); err != nil {
		t.Fatalf("RevokeSharesForFile: %v", err)
	}
	if _, err := svc.Asset(ctx, tokenOf(t, first.URL)); err == nil {
		t.Errorf("Asset(revoked) = %v, want not found", err)
	}
	if _, err := svc.Asset(ctx, tokenOf(t, og.URL)); err != nil {
		t.Errorf("Asset(other purpose) = %v, want still active", err)
	}
}

func TestShareRequiresOwnershipAndExpiry(t *testing.T) {
	svc, _, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()

	if _, err := svc.Share(ctx, otherActor, record.ID, "", 0); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("Share(foreign) = %v, want forbidden", err)
	}
	link, err := svc.Share(ctx, memberActor, record.ID, "article-cover", time.Hour)
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if link.ExpiresAt == nil || time.Until(*link.ExpiresAt) > time.Hour+time.Second {
		t.Errorf("expiry = %v, want ~1h", link.ExpiresAt)
	}
	if _, err := svc.Share(ctx, memberActor, record.ID, "", 30*time.Second); !codeIs(err, apperr.CodeInvalidArgument) {
		t.Errorf("Share(too short) = %v, want invalid argument", err)
	}
	if _, err := svc.Share(ctx, memberActor, record.ID, "Bad Purpose", 0); !codeIs(err, apperr.CodeInvalidArgument) {
		t.Errorf("Share(bad purpose) = %v, want invalid argument", err)
	}
	if _, err := svc.Share(ctx, adminActor, record.ID, "admin-share", 0); err != nil {
		t.Errorf("Share(admin) = %v", err)
	}
	if err := svc.RevokeShares(ctx, otherActor, record.ID, ""); !codeIs(err, apperr.CodeForbidden) {
		t.Errorf("RevokeShares(foreign) = %v, want forbidden", err)
	}
}

func TestAssetRejectsBadTokens(t *testing.T) {
	svc, repo, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	link, err := svc.PublicURL(context.Background(), record.ID, "")
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	token := tokenOf(t, link.URL)

	// Expired share: active row but past expires_at.
	past := time.Now().Add(-time.Minute)
	for i := range repo.shares {
		repo.shares[i].ExpiresAt = &past
	}
	if _, err := svc.Asset(context.Background(), token); err == nil {
		t.Error("Asset(expired) = nil error")
	}

	for _, bad := range []string{"", "short", strings.Repeat("a", 65), "has space in it", "bad/token"} {
		if _, err := svc.Asset(context.Background(), bad); err == nil {
			t.Errorf("Asset(%q) = nil error, want not found", bad)
		}
	}
}

func TestDeleteRevokesShares(t *testing.T) {
	svc, repo, _ := newTestService(t)
	record := uploadPNG(t, svc, memberActor)
	ctx := context.Background()
	link, err := svc.PublicURL(ctx, record.ID, "")
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	if err := svc.Delete(ctx, memberActor, record.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, share := range repo.shares {
		if share.RevokedAt == nil {
			t.Errorf("share %d survived the delete", share.ID)
		}
	}
	if _, err := svc.Asset(ctx, tokenOf(t, link.URL)); err == nil {
		t.Error("Asset after delete = nil error")
	}
}

func TestUploadQuota(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.opts.UploadDailyFiles = 1
	svc.opts.UploadDailyBytes = 1 << 20
	ctx := context.Background()
	uploadPNG(t, svc, memberActor)

	_, err := svc.Upload(ctx, memberActor, UploadInput{
		Filename: "second.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	})
	if !codeIs(err, apperr.CodeTooManyRequests) {
		t.Fatalf("second upload = %v, want daily-quota 429", err)
	}
	// Other users have their own budget.
	if _, err := svc.Upload(ctx, otherActor, UploadInput{
		Filename: "other.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); err != nil {
		t.Errorf("other user upload = %v", err)
	}

	// Byte quota is checked independently of the file count.
	svc.opts.UploadDailyFiles = 0
	svc.opts.UploadDailyBytes = int64(len(pngBytes)) * 2
	if _, err := svc.Upload(ctx, memberActor, UploadInput{
		Filename: "third.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); codeIs(err, apperr.CodeTooManyRequests) {
		t.Errorf("byte quota tripped too early: %v", err)
	}
	if _, err := svc.Upload(ctx, memberActor, UploadInput{
		Filename: "fourth.png", Size: int64(len(pngBytes)), Body: bytes.NewReader(pngBytes),
	}); !codeIs(err, apperr.CodeTooManyRequests) {
		t.Errorf("byte quota not enforced: %v", err)
	}
}

// tokenOf extracts the asset token from a share URL.
func tokenOf(t *testing.T, link string) string {
	t.Helper()
	marker := "/a/"
	idx := strings.Index(link, marker)
	if idx < 0 {
		t.Fatalf("asset URL %q has no /a/ segment", link)
	}
	rest := link[idx+len(marker):]
	token, _, _ := strings.Cut(rest, "/")
	return token
}
