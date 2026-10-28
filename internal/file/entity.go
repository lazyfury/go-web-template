// Package file is the file-management domain: object metadata, ownership,
// signed download URLs and the cross-module attachment API.
//
// Bytes live behind internal/platform/blobx (local disk or any S3-compatible
// endpoint); this package owns the rows in files/file_links and the rules that
// decide who may read, attach or delete an object. Other domains never import
// the repositories: they depend on the narrow Binder interface and get a
// *Service injected by the composition root.
package file

import "time"

// Visibility controls who may read a file. Private is the default; public is
// reserved for assets that are intentionally shareable (still served through a
// signed URL, never anonymously).
type Visibility int8

const (
	// VisibilityPrivate restricts reads to the owner and admins.
	VisibilityPrivate Visibility = 0
	// VisibilityPublic allows any authenticated caller to read the file.
	VisibilityPublic Visibility = 1
)

// Valid reports whether the visibility is one of the supported values.
func (v Visibility) Valid() bool { return v == VisibilityPrivate || v == VisibilityPublic }

// Status is the lifecycle of a file row. Deleting is a two-phase state: the
// row is marked first, the blob is removed by the housekeeping worker, and only
// then is the row hard-deleted (the reverse order would orphan unknown blobs).
type Status int8

const (
	// StatusPending marks an upload that has not been committed yet.
	StatusPending Status = 0
	// StatusReady marks a fully stored file that can be downloaded/attached.
	StatusReady Status = 1
	// StatusDeleting marks a row whose blob is waiting to be purged.
	StatusDeleting Status = 2
)

// File is the object metadata row. The bytes are addressed by
// (storage, bucket, object_key); OriginalName is display-only and never part
// of the storage path.
type File struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// Storage is the driver that wrote the object ("local" or "s3"). It is
	// stored per row so switching backends never orphans older objects.
	Storage string `gorm:"column:storage;type:varchar(16);not null;default:local" json:"storage"`
	// Bucket is the S3 bucket (empty for the local driver).
	Bucket    string `gorm:"column:bucket;type:varchar(128);not null;default:''" json:"bucket"`
	ObjectKey string `gorm:"column:object_key;type:varchar(512);not null" json:"object_key"`
	// OriginalName is the sanitized client filename (display + download name).
	OriginalName string `gorm:"column:original_name;type:varchar(255);not null" json:"original_name"`
	Ext          string `gorm:"column:ext;type:varchar(32);not null;default:''" json:"ext"`
	// MIMEType is sniffed server-side; the client's Content-Type is advisory.
	MIMEType   string     `gorm:"column:mime_type;type:varchar(128);not null;default:''" json:"mime_type"`
	SizeBytes  int64      `gorm:"column:size_bytes;not null" json:"size_bytes"`
	SHA256     string     `gorm:"column:sha256;type:char(64);not null;default:''" json:"sha256"`
	BizType    string     `gorm:"column:biz_type;type:varchar(32);not null;default:''" json:"biz_type"`
	Visibility Visibility `gorm:"column:visibility;type:smallint;not null;default:0" json:"visibility"`
	Status     Status     `gorm:"column:status;type:smallint;not null;default:1" json:"status"`
	// OwnerID is the uploading user. Nullable for system-generated files.
	OwnerID   *uint64    `gorm:"column:owner_id" json:"owner_id,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at" json:"-"`
}

// TableName keeps the table name explicit.
func (File) TableName() string { return "files" }

// Link attaches a file to a business resource. OwnerType is defined by the
// consuming module ("user.avatar", "message.attachment", ...) so this table
// never needs a migration when a new module starts using files.
type Link struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	FileID    uint64     `gorm:"column:file_id;not null" json:"file_id"`
	OwnerType string     `gorm:"column:owner_type;type:varchar(32);not null" json:"owner_type"`
	OwnerID   uint64     `gorm:"column:owner_id;not null" json:"owner_id"`
	Slot      string     `gorm:"column:slot;type:varchar(32);not null;default:''" json:"slot"`
	CreatedAt time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at" json:"-"`
}

// TableName keeps the table name explicit.
func (Link) TableName() string { return "file_links" }

// Share is a long-lived public capability URL for one file. It is the
// "article cover / avatar / product image" half of the download story: unlike a
// signed URL it can be permanent and revoked, at the cost of one indexed lookup
// per request (absorption by the HTTP cache is the mitigation).
type Share struct {
	ID      uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	FileID  uint64 `gorm:"column:file_id;not null" json:"file_id"`
	Token   string `gorm:"column:token;type:varchar(64);not null" json:"-"`
	Purpose string `gorm:"column:purpose;type:varchar(32);not null;default:asset" json:"purpose"`
	// ExpiresAt is nil for a permanent share.
	ExpiresAt *time.Time `gorm:"column:expires_at" json:"expires_at,omitempty"`
	RevokedAt *time.Time `gorm:"column:revoked_at" json:"-"`
	CreatedBy *uint64    `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;not null" json:"created_at"`
}

// TableName keeps the table name explicit.
func (Share) TableName() string { return "file_shares" }

// IsOwner reports whether userID uploaded the file.
func (f *File) IsOwner(userID uint64) bool {
	return f.OwnerID != nil && *f.OwnerID == userID
}
