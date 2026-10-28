package file

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/blobx"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// DownloadHandler serves local-driver files through the app using signed URLs.
//
// The URL is issued by blobx.Local and never touches /api/v1: the signature is
// the credential, so the route stays outside the authenticated group (the
// "deny by default" rule is about API routes, not about capability URLs).
type DownloadHandler struct {
	svc    *Service
	secret []byte
	now    func() time.Time
}

// NewDownloadHandler creates the signed-download handler. An empty secret
// disables the route (it answers 404, which is the correct response when the
// deployment uses the S3 driver and never issues local URLs).
func NewDownloadHandler(svc *Service, secret []byte) *DownloadHandler {
	return &DownloadHandler{svc: svc, secret: append([]byte(nil), secret...), now: time.Now}
}

// ServeLocal handles GET /files/:id/:disposition/:exp/:sig.
func (h *DownloadHandler) ServeLocal(c *gin.Context) {
	if len(h.secret) == 0 {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	expires, err := strconv.ParseInt(c.Param("exp"), 10, 64)
	if err != nil {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	disposition := c.Param("disposition")
	if disposition != "inline" && disposition != "attachment" {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	record, err := h.svc.FindForDownload(c.Request.Context(), id)
	if err != nil {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	// A bad signature answers 404, not 403: the response must not confirm which
	// file ids exist.
	if err := blobx.VerifyLocalURL(h.secret, record.ObjectKey, expires, disposition, c.Param("sig"), h.now()); err != nil {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}
	if record.Storage != blobx.DriverLocal {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "not found")
		return
	}

	reader, info, err := h.svc.Open(c.Request.Context(), record)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = reader.Close() }()

	// Content-Type always reflects the real type; whether the browser renders or
	// downloads is decided by Content-Disposition, not by lying about the type.
	// `inline` is still restricted to the inline allowlist when the URL is issued.
	contentType := record.MIMEType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	remaining := expires - h.now().Unix()
	if remaining < 0 {
		remaining = 0
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", contentDisposition(disposition, record.OriginalName))
	c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", remaining))
	c.Header("Content-Type", contentType)

	if seeker, ok := reader.(io.ReadSeeker); ok {
		// ServeContent adds Range support (video/PDF seeking) and Last-Modified.
		http.ServeContent(c.Writer, c.Request, record.OriginalName, record.CreatedAt, seeker)
		return
	}
	c.Status(http.StatusOK)
	c.Header("Content-Length", strconv.FormatInt(info.Size, 10))
	if c.Request.Method != http.MethodHead {
		if _, err := io.Copy(c.Writer, reader); err != nil {
			// Headers are already sent; nothing left but to stop writing.
			return
		}
	}
}

// contentDisposition builds an RFC 6266 value with an ASCII fallback plus the
// UTF-8 filename* form so non-ASCII names survive.
func contentDisposition(disposition, filename string) string {
	if filename == "" {
		return disposition
	}
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, filename)
	return fmt.Sprintf(`%s; filename=%q; filename*=UTF-8''%s`, disposition, fallback, url.PathEscape(filename))
}
