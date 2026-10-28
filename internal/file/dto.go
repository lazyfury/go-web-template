package file

import (
	"strconv"
	"time"
)

// UploadRequest is the multipart/form-data payload for POST /files. The file
// part itself is binary; biz_type/visibility are optional text fields and may
// also arrive after the file.
type UploadRequest struct {
	File       string      `json:"file" binding:"required" format:"binary" description:"File content (max storage.max_upload_bytes)"`
	BizType    string      `json:"biz_type" binding:"omitempty,max=32" description:"Business type, e.g. avatar or attachment"`
	Visibility *Visibility `json:"visibility" binding:"omitempty,oneof=0 1" description:"0=private (default), 1=public to authenticated users"`
}

// UpdateVisibilityRequest is the payload for PATCH /files/:id.
//
// Visibility is a pointer on purpose: `binding:"required"` rejects the zero
// value, and VisibilityPrivate IS zero — switching a file back to private would
// fail validation. The pointer keeps "field missing" distinguishable from
// "explicitly 0".
type UpdateVisibilityRequest struct {
	Visibility *Visibility `json:"visibility" binding:"required,oneof=0 1"`
}

// ShareRequest is the payload for POST /files/:id/share. An empty purpose
// defaults to "asset"; expires_in_seconds 0 (or omitted) means permanent.
type ShareRequest struct {
	Purpose          string `json:"purpose" binding:"omitempty,max=32" description:"Purpose scope, e.g. article-cover or og-image (default asset)"`
	ExpiresInSeconds *int   `json:"expires_in_seconds" binding:"omitempty,min=60" description:"Link lifetime in seconds; omit or 0 for a permanent link"`
}

// StoredFileDTO is the HTTP representation of a file. Storage keys and buckets stay
// internal: clients address files by id and get a signed URL.
type StoredFileDTO struct {
	ID         uint64     `json:"id"`
	Name       string     `json:"name"`
	MIMEType   string     `json:"mime_type"`
	Ext        string     `json:"ext"`
	SizeBytes  int64      `json:"size_bytes"`
	BizType    string     `json:"biz_type"`
	Visibility Visibility `json:"visibility" enum:"0,1"`
	SHA256     string     `json:"sha256"`
	OwnerID    *uint64    `json:"owner_id,omitempty"`
	// InlineAllowed tells the UI whether the browser can render this type inline
	// (image/pdf/text/video). Others are always served as attachments.
	InlineAllowed bool      `json:"inline_allowed"`
	DownloadPath  string    `json:"download_path" description:"Relative API path returning a signed URL"`
	CreatedAt     time.Time `json:"created_at"`
}

// TextPreviewResponse is a bounded plain-text preview (GET /files/:id/text).
// The JSON envelope is deliberate: an iframe/fetch of the signed URL would hit
// Content-Disposition/CORS, while this stays same-origin and authenticated.
type TextPreviewResponse struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	SizeBytes int64  `json:"size_bytes"`
}

// Page is one page of files.
type Page struct {
	Items    []StoredFileDTO `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// URLResponse is a signed download URL.
type URLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func toStoredFileDTO(f *File, inlineAllowed bool) StoredFileDTO {
	return StoredFileDTO{
		ID:            f.ID,
		Name:          f.OriginalName,
		MIMEType:      f.MIMEType,
		Ext:           f.Ext,
		SizeBytes:     f.SizeBytes,
		BizType:       f.BizType,
		Visibility:    f.Visibility,
		SHA256:        f.SHA256,
		OwnerID:       f.OwnerID,
		InlineAllowed: inlineAllowed,
		DownloadPath:  "/api/v1/files/" + strconv.FormatUint(f.ID, 10) + "/url",
		CreatedAt:     f.CreatedAt,
	}
}

// toStoredFileDTO fills the UI hints that depend on configuration.
func (s *Service) toStoredFileDTO(f *File) StoredFileDTO {
	return toStoredFileDTO(f, s.InlineAllowed(f.MIMEType))
}

func (s *Service) toStoredFileDTOs(files []File) []StoredFileDTO {
	out := make([]StoredFileDTO, 0, len(files))
	for i := range files {
		out = append(out, s.toStoredFileDTO(&files[i]))
	}
	return out
}
