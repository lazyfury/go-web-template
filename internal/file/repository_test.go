package file

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/testutil"
)

func newTestRepository(t *testing.T) (Repository, *gorm.DB) {
	t.Helper()
	db := testutil.NewSQLiteDB(t, &File{}, &Link{}, &Share{})
	// AutoMigrate does not know about the partial unique index from the
	// migration; create it here so the idempotent-attach contract is actually
	// enforced by the database (same predicate as 000010).
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_file_links_unique
		ON file_links (owner_type, owner_id, slot, file_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatalf("create partial unique index: %v", err)
	}
	// Same for the share table: one active share per (file, purpose).
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_file_shares_file_purpose
		ON file_shares (file_id, purpose) WHERE revoked_at IS NULL`).Error; err != nil {
		t.Fatalf("create share unique index: %v", err)
	}
	return NewRepository(db), db
}

func createFile(t *testing.T, repo Repository, ownerID uint64, name, bizType string) *File {
	t.Helper()
	owner := ownerID
	record := &File{
		Storage:      "local",
		Bucket:       "",
		ObjectKey:    "file/202601/" + name + ".png",
		OriginalName: name,
		Ext:          ".png",
		MIMEType:     "image/png",
		SizeBytes:    10,
		SHA256:       "abc",
		BizType:      bizType,
		Status:       StatusReady,
		OwnerID:      &owner,
	}
	if err := repo.Create(context.Background(), record); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return record
}

func TestRepositoryCreateFindAndSoftDelete(t *testing.T) {
	repo, _ := newTestRepository(t)
	ctx := context.Background()
	record := createFile(t, repo, 7, "logo", "avatar")

	found, err := repo.FindByID(ctx, record.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.OriginalName != "logo" || found.Status != StatusReady {
		t.Errorf("found = %+v", found)
	}

	at := time.Now()
	if err := repo.MarkDeleting(ctx, record.ID, at); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	// Soft-deleted rows are invisible.
	if _, err := repo.FindByID(ctx, record.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("FindByID after delete = %v, want not found", err)
	}
	// Marking twice is not an error-free no-op: the row is already gone from the
	// caller's point of view.
	if err := repo.MarkDeleting(ctx, record.ID, at); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("MarkDeleting twice = %v, want not found", err)
	}
	if err := repo.HardDelete(ctx, record.ID); err != nil {
		t.Errorf("HardDelete: %v", err)
	}
	if err := repo.HardDelete(ctx, record.ID); err != nil {
		t.Errorf("HardDelete(missing) = %v, want nil (idempotent)", err)
	}
}

func TestRepositoryListFiltersAndPagination(t *testing.T) {
	repo, _ := newTestRepository(t)
	ctx := context.Background()
	createFile(t, repo, 7, "avatar", "avatar")
	createFile(t, repo, 7, "report", "attachment")
	createFile(t, repo, 8, "other", "avatar")

	all, total, err := repo.List(ctx, ListFilter{Page: 1, PageSize: 10})
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("List(all) = %d/%d, %v", total, len(all), err)
	}

	ownerID := uint64(7)
	own, total, err := repo.List(ctx, ListFilter{OwnerID: &ownerID, Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(own) != 2 {
		t.Fatalf("List(owner) = %d/%d, %v", total, len(own), err)
	}
	for _, f := range own {
		if f.OwnerID == nil || *f.OwnerID != ownerID {
			t.Errorf("owner filter leaked file %+v", f)
		}
	}

	page, total, err := repo.List(ctx, ListFilter{Page: 2, PageSize: 2})
	if err != nil || total != 3 || len(page) != 1 {
		t.Fatalf("List(page 2) = %d/%d, %v", total, len(page), err)
	}

	byBiz, total, err := repo.List(ctx, ListFilter{BizType: "avatar", Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(byBiz) != 2 {
		t.Fatalf("List(biz) = %d/%d, %v", total, len(byBiz), err)
	}

	byKeyword, total, err := repo.List(ctx, ListFilter{Keyword: "REPORT", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(byKeyword) != 1 || byKeyword[0].OriginalName != "report" {
		t.Fatalf("List(keyword, case-insensitive) = %d/%d, %v", total, len(byKeyword), err)
	}
}

func TestRepositoryLinksAreIdempotentAndScoped(t *testing.T) {
	repo, _ := newTestRepository(t)
	ctx := context.Background()
	record := createFile(t, repo, 7, "logo", "avatar")

	link := &Link{FileID: record.ID, OwnerType: "user.avatar", OwnerID: 7, Slot: "avatar"}
	for i := 0; i < 3; i++ {
		if err := repo.CreateLink(ctx, link); err != nil {
			t.Fatalf("CreateLink #%d: %v", i, err)
		}
	}
	if count, err := repo.CountActiveLinks(ctx, record.ID); err != nil || count != 1 {
		t.Fatalf("active links = %d, %v; want 1", count, err)
	}

	files, err := repo.ListFilesByOwner(ctx, "user.avatar", 7)
	if err != nil || len(files) != 1 || files[0].ID != record.ID {
		t.Fatalf("ListFilesByOwner = %+v, %v", files, err)
	}
	if files, err := repo.ListFilesByOwner(ctx, "user.avatar", 8); err != nil || len(files) != 0 {
		t.Fatalf("ListFilesByOwner(other) = %+v, %v", files, err)
	}

	// Detach only affects the requested slot.
	other := &Link{FileID: record.ID, OwnerType: "user.avatar", OwnerID: 7, Slot: "gallery"}
	if err := repo.CreateLink(ctx, other); err != nil {
		t.Fatalf("CreateLink(gallery): %v", err)
	}
	if err := repo.SoftDeleteLinks(ctx, record.ID, "user.avatar", 7, "avatar", time.Now()); err != nil {
		t.Fatalf("SoftDeleteLinks: %v", err)
	}
	if count, _ := repo.CountActiveLinks(ctx, record.ID); count != 1 {
		t.Errorf("active links after slot detach = %d, want 1", count)
	}
	if err := repo.SoftDeleteLinksByOwner(ctx, "user.avatar", 7, time.Now()); err != nil {
		t.Fatalf("SoftDeleteLinksByOwner: %v", err)
	}
	if count, _ := repo.CountActiveLinks(ctx, record.ID); count != 0 {
		t.Errorf("active links after owner detach = %d, want 0", count)
	}
}

func TestRepositoryOrphansAndPurgeQueries(t *testing.T) {
	repo, db := newTestRepository(t)
	ctx := context.Background()
	linked := createFile(t, repo, 7, "linked", "avatar")
	orphan := createFile(t, repo, 7, "orphan", "avatar")
	if err := repo.CreateLink(ctx, &Link{FileID: linked.ID, OwnerType: "user.avatar", OwnerID: 7, Slot: "avatar"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Backdate both rows so the cutoff selects them.
	past := time.Now().Add(-48 * time.Hour)
	if err := db.Model(&File{}).Where("id IN ?", []uint64{linked.ID, orphan.ID}).Update("created_at", past).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// A fresh cutoff (now-1h) excludes rows created 48h ago only if they are
	// newer than it; here they are older, so both are candidates but only the
	// unlinked one is an orphan.
	orphans, err := repo.ListOrphans(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("ListOrphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0].ID != orphan.ID {
		t.Fatalf("orphans = %+v, want only %d", orphans, orphan.ID)
	}
	// A cutoff older than the rows excludes everything.
	if orphans, _ := repo.ListOrphans(ctx, time.Now().Add(-72*time.Hour), 10); len(orphans) != 0 {
		t.Errorf("ListOrphans(cutoff before creation) = %d rows", len(orphans))
	}

	if at := time.Now(); repo.MarkDeleting(ctx, orphan.ID, at) != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	deleting, err := repo.ListByStatusBefore(ctx, StatusDeleting, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ListByStatusBefore: %v", err)
	}
	if len(deleting) != 1 || deleting[0].ID != orphan.ID {
		t.Fatalf("deleting rows = %+v", deleting)
	}
}

func TestRepositoryUpdateVisibility(t *testing.T) {
	repo, _ := newTestRepository(t)
	ctx := context.Background()
	record := createFile(t, repo, 7, "logo", "avatar")
	if err := repo.UpdateVisibility(ctx, record.ID, VisibilityPublic); err != nil {
		t.Fatalf("UpdateVisibility: %v", err)
	}
	found, err := repo.FindByID(ctx, record.ID)
	if err != nil || found.Visibility != VisibilityPublic {
		t.Fatalf("visibility = %v, %v", found.Visibility, err)
	}
	if err := repo.UpdateVisibility(ctx, 9999, VisibilityPublic); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("UpdateVisibility(missing) = %v, want not found", err)
	}
}

func TestRepositoryShares(t *testing.T) {
	repo, _ := newTestRepository(t)
	ctx := context.Background()
	record := createFile(t, repo, 7, "logo", "avatar")
	now := time.Now()

	share := &Share{FileID: record.ID, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Purpose: "asset"}
	if err := repo.CreateShare(ctx, share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	// The partial unique index allows only one active share per (file, purpose).
	if err := repo.CreateShare(ctx, &Share{FileID: record.ID, Token: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Purpose: "asset"}); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("duplicate share = %v, want conflict", err)
	}

	found, err := repo.FindActiveShareByToken(ctx, share.Token, now)
	if err != nil || found.FileID != record.ID {
		t.Fatalf("FindActiveShareByToken = %+v, %v", found, err)
	}
	if _, err := repo.FindActiveShareByToken(ctx, "missing", now); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("missing token = %v, want not found", err)
	}

	// Expiry is part of the active predicate.
	expired := &Share{FileID: record.ID, Token: "ccccccccccccccccccccccccccccccccccccccccccc", Purpose: "og", ExpiresAt: ptrTime(now.Add(-time.Minute))}
	if err := repo.CreateShare(ctx, expired); err != nil {
		t.Fatalf("CreateShare(expired): %v", err)
	}
	if _, err := repo.FindActiveShareByToken(ctx, expired.Token, now); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expired token = %v, want not found", err)
	}

	// Revoking frees the (file, purpose) slot again.
	if err := repo.RevokeShares(ctx, record.ID, "asset", now); err != nil {
		t.Fatalf("RevokeShares: %v", err)
	}
	if _, err := repo.FindActiveShareByToken(ctx, share.Token, now); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("revoked token = %v, want not found", err)
	}
	if err := repo.CreateShare(ctx, &Share{FileID: record.ID, Token: "ddddddddddddddddddddddddddddddddddddddddddd", Purpose: "asset"}); err != nil {
		t.Errorf("CreateShare after revoke = %v", err)
	}
}

func TestRepositoryUsageSince(t *testing.T) {
	repo, db := newTestRepository(t)
	ctx := context.Background()
	first := createFile(t, repo, 7, "a", "avatar")
	second := createFile(t, repo, 7, "b", "avatar")
	createFile(t, repo, 8, "c", "avatar")

	// Backdate one row outside the window.
	old := time.Now().Add(-48 * time.Hour)
	if err := db.Model(&File{}).Where("id = ?", first.ID).Update("created_at", old).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
	files, bytes, err := repo.UsageSince(ctx, 7, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("UsageSince: %v", err)
	}
	if files != 1 || bytes != second.SizeBytes {
		t.Errorf("usage = %d files / %d bytes, want 1 / %d", files, bytes, second.SizeBytes)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
