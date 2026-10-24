package auth

import (
	"context"
	"testing"
	"time"

	"github.com/example/go-web-template/internal/testutil"
)

func newTestRepository(t *testing.T) Repository {
	t.Helper()
	db := testutil.NewSQLiteDB(t, &RefreshToken{})
	return NewRepository(db)
}

func TestPurgeExpiredDeletesOnlyRowsPastTheCutoff(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	expired := &RefreshToken{
		UserID:           1,
		TokenHash:        "expired",
		ExpiresAt:        now.Add(-8 * 24 * time.Hour),
		SessionStartedAt: now.Add(-38 * 24 * time.Hour),
		CreatedAt:        now.Add(-30 * 24 * time.Hour),
	}
	revokedButUnexpired := &RefreshToken{
		UserID:           1,
		TokenHash:        "revoked",
		ExpiresAt:        now.Add(24 * time.Hour),
		RevokedAt:        ptr(now.Add(-time.Hour)),
		RevokedReason:    RevokeLogout,
		SessionStartedAt: now,
		CreatedAt:        now.Add(-time.Hour),
	}
	active := &RefreshToken{
		UserID:           2,
		TokenHash:        "active",
		ExpiresAt:        now.Add(30 * 24 * time.Hour),
		SessionStartedAt: now,
		CreatedAt:        now,
	}
	for _, token := range []*RefreshToken{expired, revokedButUnexpired, active} {
		if err := repo.Create(ctx, token); err != nil {
			t.Fatalf("Create(%s): %v", token.TokenHash, err)
		}
	}

	cutoff := now.Add(-7 * 24 * time.Hour) // retention window of 7 days
	deleted, err := repo.PurgeExpired(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the expired row)", deleted)
	}

	for _, hash := range []string{"revoked", "active"} {
		if _, err := repo.FindByHash(ctx, hash); err != nil {
			t.Errorf("FindByHash(%s) = %v, want it kept", hash, err)
		}
	}
	if _, err := repo.FindByHash(ctx, "expired"); err == nil {
		t.Error("expired row survived the purge")
	}
}

func ptr[T any](v T) *T { return &v }
