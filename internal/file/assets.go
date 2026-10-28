package file

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// AssetHandler serves long-lived public asset URLs: GET|HEAD /a/{token}/{name}.
//
// This route is deliberately outside /api/v1: the token is the credential (a
// capability URL), exactly like the signed local-download route. It is also the
// only route that sets a public Cache-Control, so a CDN can absorb the traffic
// and the app only sees cache misses.
type AssetHandler struct {
	svc *Service
	now func() time.Time
}

// NewAssetHandler creates the asset handler.
func NewAssetHandler(svc *Service) *AssetHandler {
	return &AssetHandler{svc: svc, now: time.Now}
}

// ServeAsset handles GET|HEAD /a/:token/:name.
func (h *AssetHandler) ServeAsset(c *gin.Context) {
	record, err := h.svc.Asset(c.Request.Context(), c.Param("token"))
	if err != nil {
		observeAssetRequest("not_found")
		// Revoked, expired and unknown tokens are indistinguishable by design.
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	reader, info, err := h.svc.Open(c.Request.Context(), record)
	if err != nil {
		observeAssetRequest("error")
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	defer func() { _ = reader.Close() }()

	// Public assets are content-addressed by their bytes: a replaced file gets a
	// new row (and therefore a new token), so a long TTL cannot serve stale
	// content under a live URL. Inline is still restricted to the allowlist.
	cacheTTL := h.svc.AssetCacheTTL()
	inline := h.svc.InlineAllowed(record.MIMEType)
	contentType := record.MIMEType
	disposition := "inline"
	if !inline {
		contentType = "application/octet-stream"
		disposition = "attachment"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Header("X-Content-Type-Options", "nosniff")
	// Asset URLs end up in shared HTML/mails: never leak them through Referer to
	// third parties, and never let a third-party document become the referrer.
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Disposition", contentDisposition(disposition, record.OriginalName))
	c.Header("Content-Type", contentType)
	c.Header("ETag", assetETag(record))
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d, stale-while-revalidate=%d",
		int64(cacheTTL.Seconds()), int64((2*cacheTTL).Seconds())))

	if seeker, ok := reader.(io.ReadSeeker); ok {
		// ServeContent handles If-None-Match/If-Modified-Since (304), HEAD and
		// Range for free; the ETag above is what makes the 304 cheap.
		http.ServeContent(c.Writer, c.Request, record.OriginalName, record.CreatedAt, seeker)
		observeAssetRequest("served")
		return
	}
	c.Header("Content-Length", strconv.FormatInt(info.Size, 10))
	c.Status(http.StatusOK)
	observeAssetRequest("served")
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(c.Writer, reader)
}

// assetETag is a strong validator derived from the content hash. Falls back to
// id+size for rows written before hashing existed.
func assetETag(record *File) string {
	value := record.SHA256
	if value == "" {
		value = fmt.Sprintf("%d-%d", record.ID, record.SizeBytes)
	}
	if len(value) > 32 {
		value = value[:32]
	}
	return `"` + value + `"`
}

// InlineAllowed reports whether a MIME type may render inline.
func (s *Service) InlineAllowed(mimeType string) bool {
	_, ok := s.inlineMIME[strings.ToLower(mimeType)]
	return ok
}
