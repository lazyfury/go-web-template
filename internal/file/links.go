package file

import (
	"context"
	"strings"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
)

// Ref identifies a business resource that files are attached to. OwnerType is
// owned by the consuming module ("user.avatar", "message.attachment", ...) and
// is the only coupling between that module and file metadata.
type Ref struct {
	OwnerType string
	OwnerID   uint64
}

// AttachInput attaches one file to a resource slot.
type AttachInput struct {
	Ref
	FileID uint64
	// Slot is "" for an unordered attachment set, or a name like "avatar" for a
	// single-value slot.
	Slot string
}

// Binder is the cross-module contract for file attachments. Other domains
// declare their own narrow copy of the methods they need and receive a
// *Service from the composition root, so they never import this package's
// repositories (or each other).
type Binder interface {
	// Attach verifies the file exists, is ready and belongs to the actor, then
	// links it to the resource (idempotent).
	Attach(ctx context.Context, actor authkit.Principal, in AttachInput) (*File, error)
	// Detach removes one attachment.
	Detach(ctx context.Context, actor authkit.Principal, in AttachInput) error
	// ListByOwner returns the files attached to a resource, in attach order.
	ListByOwner(ctx context.Context, ownerType string, ownerID uint64) ([]File, error)
	// DeleteByOwner detaches every file of a resource (call it when the resource
	// itself is deleted). Unreferenced files are purged by the janitor.
	DeleteByOwner(ctx context.Context, ownerType string, ownerID uint64) error
	// SignedURL is the trusted internal download URL (no actor check): it is for
	// servers rendering a resource they already authorized, never for HTTP
	// handlers that take a caller-supplied id.
	SignedURL(ctx context.Context, fileID uint64, disposition string) (URLResult, error)
	// PublicURL returns a long-lived, revocable asset URL (article cover, avatar,
	// product image). It is also trusted-internal and meant for rendering.
	PublicURL(ctx context.Context, fileID uint64, purpose string) (AssetLink, error)
	// RevokeShares invalidates the public links of a file (all purposes when
	// purpose is empty). Call it when the resource is deleted or the image is
	// replaced.
	RevokeSharesForFile(ctx context.Context, fileID uint64, purpose string) error
}

var _ Binder = (*Service)(nil)

// Attach implements Binder.
func (s *Service) Attach(ctx context.Context, actor authkit.Principal, in AttachInput) (*File, error) {
	if err := validateAttachInput(in); err != nil {
		return nil, err
	}
	record, err := s.repo.FindByID(ctx, in.FileID)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusReady {
		return nil, apperr.New(apperr.CodeConflict, "file is not ready")
	}
	// Attaching someone else's public file to your resource would let a member
	// "launder" an object it cannot delete; require write access.
	if !canWrite(actor, record) {
		return nil, apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	link := &Link{
		FileID:    in.FileID,
		OwnerType: in.OwnerType,
		OwnerID:   in.OwnerID,
		Slot:      in.Slot,
	}
	if err := s.repo.CreateLink(ctx, link); err != nil {
		return nil, err
	}
	return record, nil
}

// Detach implements Binder.
func (s *Service) Detach(ctx context.Context, actor authkit.Principal, in AttachInput) error {
	if err := validateAttachInput(in); err != nil {
		return err
	}
	record, err := s.repo.FindByID(ctx, in.FileID)
	if err != nil {
		return err
	}
	if !canWrite(actor, record) {
		return apperr.New(apperr.CodeForbidden, "no access to this file")
	}
	return s.repo.SoftDeleteLinks(ctx, in.FileID, in.OwnerType, in.OwnerID, in.Slot, s.opts.Now())
}

// ListByOwner implements Binder.
func (s *Service) ListByOwner(ctx context.Context, ownerType string, ownerID uint64) ([]File, error) {
	if err := validateRef(Ref{OwnerType: ownerType, OwnerID: ownerID}); err != nil {
		return nil, err
	}
	return s.repo.ListFilesByOwner(ctx, ownerType, ownerID)
}

// DeleteByOwner implements Binder.
func (s *Service) DeleteByOwner(ctx context.Context, ownerType string, ownerID uint64) error {
	if err := validateRef(Ref{OwnerType: ownerType, OwnerID: ownerID}); err != nil {
		return err
	}
	return s.repo.SoftDeleteLinksByOwner(ctx, ownerType, ownerID, s.opts.Now())
}

// SignedURL implements Binder.
func (s *Service) SignedURL(ctx context.Context, fileID uint64, disposition string) (URLResult, error) {
	record, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return URLResult{}, err
	}
	if record.Status != StatusReady {
		return URLResult{}, apperr.New(apperr.CodeNotFound, "file not found")
	}
	return s.signedURLFor(ctx, record, disposition, 0)
}

func validateAttachInput(in AttachInput) error {
	if err := validateRef(in.Ref); err != nil {
		return err
	}
	if in.FileID == 0 {
		return apperr.New(apperr.CodeInvalidArgument, "file_id is required")
	}
	if in.Slot != "" && !validOwnerToken(in.Slot) {
		return apperr.New(apperr.CodeInvalidArgument, "invalid slot")
	}
	if len(in.Slot) > 32 {
		return apperr.New(apperr.CodeInvalidArgument, "slot must be at most 32 characters")
	}
	return nil
}

func validateRef(ref Ref) error {
	if !validOwnerToken(ref.OwnerType) || len(ref.OwnerType) > 32 {
		return apperr.New(apperr.CodeInvalidArgument, "invalid owner_type")
	}
	if ref.OwnerID == 0 {
		return apperr.New(apperr.CodeInvalidArgument, "owner_id is required")
	}
	return nil
}

// validOwnerToken keeps owner_type/slot values to a machine-readable token so
// they cannot be used to smuggle formatting or SQL fragments.
func validOwnerToken(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
		if i == 0 && !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz", r) {
			return false
		}
	}
	return true
}
