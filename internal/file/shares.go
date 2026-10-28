package file

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
)

// DefaultSharePurpose is used when a caller does not say what the link is for.
const DefaultSharePurpose = "asset"

// AssetLink is a long-lived public asset URL.
type AssetLink struct {
	URL       string     `json:"url"`
	Purpose   string     `json:"purpose"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// PublicURL returns (creating it if needed) the long-lived asset URL for a file.
// It is the trusted internal API used by other domains when rendering their own
// resources; it performs no actor check because there is no HTTP caller.
func (s *Service) PublicURL(ctx context.Context, fileID uint64, purpose string) (AssetLink, error) {
	record, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return AssetLink{}, err
	}
	if record.Status != StatusReady {
		return AssetLink{}, apperr.New(apperr.CodeNotFound, "file not found")
	}
	return s.ensureShare(ctx, record, purpose, nil)
}

// Share is the HTTP-facing variant: the caller must own the file (or be admin).
// expiresIn == 0 means "permanent".
func (s *Service) Share(ctx context.Context, actor authkit.Principal, fileID uint64, purpose string, expiresIn time.Duration) (AssetLink, error) {
	if expiresIn < 0 || (expiresIn > 0 && expiresIn < time.Minute) {
		return AssetLink{}, apperr.New(apperr.CodeInvalidArgument, "expires_in must be 0 (permanent) or at least 60 seconds")
	}
	record, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return AssetLink{}, err
	}
	if !canWrite(actor, record) {
		return AssetLink{}, apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	if record.Status != StatusReady {
		return AssetLink{}, apperr.New(apperr.CodeConflict, "file is not ready")
	}
	var expiresAt *time.Time
	if expiresIn > 0 {
		at := s.opts.Now().Add(expiresIn)
		expiresAt = &at
	}
	return s.ensureShare(ctx, record, purpose, expiresAt)
}

// RevokeShares is the HTTP-facing revoke: ownership required. An empty purpose
// revokes every active share of the file.
func (s *Service) RevokeShares(ctx context.Context, actor authkit.Principal, fileID uint64, purpose string) error {
	record, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return err
	}
	if !canWrite(actor, record) {
		return apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	if purpose != "" {
		normalized, err := normalizePurpose(purpose)
		if err != nil {
			return err
		}
		purpose = normalized
	}
	return s.repo.RevokeShares(ctx, fileID, purpose, s.opts.Now())
}

// RevokeSharesForFile is the trusted internal revoke (resource deletion).
func (s *Service) RevokeSharesForFile(ctx context.Context, fileID uint64, purpose string) error {
	return s.repo.RevokeShares(ctx, fileID, purpose, s.opts.Now())
}

// Asset resolves a public asset token to a ready file. A revoked, expired or
// unknown token is the same ErrNotFound: the response must not tell an attacker
// which tokens used to exist.
func (s *Service) Asset(ctx context.Context, token string) (*File, error) {
	if !validToken(token) {
		return nil, apperr.ErrNotFound
	}
	share, err := s.repo.FindActiveShareByToken(ctx, token, s.opts.Now())
	if err != nil {
		return nil, err
	}
	record, err := s.repo.FindByID(ctx, share.FileID)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusReady {
		return nil, apperr.ErrNotFound
	}
	return record, nil
}

// AssetBaseURL is the origin printed into asset links.
func (s *Service) AssetBaseURL() string { return s.opts.AssetBaseURL }

// AssetCacheTTL is the browser/CDN cache lifetime of an asset response.
func (s *Service) AssetCacheTTL() time.Duration { return s.opts.AssetCacheTTL }

// ensureShare is idempotent: one active share per (file, purpose).
func (s *Service) ensureShare(ctx context.Context, record *File, purpose string, expiresAt *time.Time) (AssetLink, error) {
	purpose, err := normalizePurpose(purpose)
	if err != nil {
		return AssetLink{}, err
	}
	if s.opts.AssetBaseURL == "" {
		return AssetLink{}, apperr.New(apperr.CodeInternal, "storage.assets.base_url is not configured")
	}
	share, err := s.repo.FindActiveShareByPurpose(ctx, record.ID, purpose, s.opts.Now())
	if err != nil && !errors.Is(err, apperr.ErrNotFound) {
		return AssetLink{}, err
	}
	if errors.Is(err, apperr.ErrNotFound) {
		token, err := randomToken(s.opts.PublicTokenBytes)
		if err != nil {
			return AssetLink{}, apperr.Wrap(apperr.CodeInternal, "generate asset token failed", err)
		}
		owner := (*uint64)(nil)
		if record.OwnerID != nil {
			owner = record.OwnerID
		}
		share = &Share{
			FileID:    record.ID,
			Token:     token,
			Purpose:   purpose,
			ExpiresAt: expiresAt,
			CreatedBy: owner,
		}
		if err := s.repo.CreateShare(ctx, share); err != nil {
			// Lost a race with a concurrent request: reuse the winner's row.
			existing, findErr := s.repo.FindActiveShareByPurpose(ctx, record.ID, purpose, s.opts.Now())
			if findErr != nil {
				return AssetLink{}, err
			}
			share = existing
		}
	}
	return AssetLink{
		URL:       s.assetURL(share.Token, record.OriginalName),
		Purpose:   share.Purpose,
		ExpiresAt: share.ExpiresAt,
	}, nil
}

func (s *Service) assetURL(token, filename string) string {
	return fmt.Sprintf("%s/a/%s/%s", strings.TrimRight(s.opts.AssetBaseURL, "/"), token, url.PathEscape(filename))
}

// checkUploadQuota enforces the rolling per-user upload budget.
func (s *Service) checkUploadQuota(ctx context.Context, ownerID uint64) error {
	if s.opts.UploadDailyFiles <= 0 && s.opts.UploadDailyBytes <= 0 {
		return nil
	}
	files, bytes, err := s.repo.UsageSince(ctx, ownerID, s.opts.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if s.opts.UploadDailyFiles > 0 && files >= int64(s.opts.UploadDailyFiles) {
		return apperr.New(apperr.CodeTooManyRequests, fmt.Sprintf("daily upload count limit reached (%d/24h)", s.opts.UploadDailyFiles))
	}
	if s.opts.UploadDailyBytes > 0 && bytes >= s.opts.UploadDailyBytes {
		return apperr.New(apperr.CodeTooManyRequests, fmt.Sprintf("daily upload byte limit reached (%d bytes/24h)", s.opts.UploadDailyBytes))
	}
	return nil
}

// normalizePurpose keeps purpose a small machine-readable token (same rule as
// biz_type) and defaults to "asset".
func normalizePurpose(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultSharePurpose, nil
	}
	if len(value) > 32 {
		return "", apperr.New(apperr.CodeInvalidArgument, "purpose must be at most 32 characters")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return "", apperr.New(apperr.CodeInvalidArgument, "purpose may only contain a-z, 0-9, _, - and .")
	}
	return value, nil
}

// randomToken returns n bytes of crypto/rand as base64url (no padding).
func randomToken(n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// validToken rejects anything that is not the exact shape we issue, so a
// scanner cannot turn /a/{token} into a cheap database probe with odd input.
func validToken(token string) bool {
	if len(token) < 16 || len(token) > 64 {
		return false
	}
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
