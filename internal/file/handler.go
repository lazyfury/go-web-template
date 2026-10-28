package file

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// HandlerOptions carries the upload sizing the transport layer needs. They come
// from config, never from the request.
type HandlerOptions struct {
	// MaxUploadBytes is the per-file cap; multipart parsing uses it as its memory
	// budget. The request body itself is capped by the route-level BodyLimit.
	MaxUploadBytes int64
}

// Handler exposes the file use-cases over HTTP.
type Handler struct {
	svc *Service
	opt HandlerOptions
}

// NewHandler creates a Handler.
func NewHandler(svc *Service, opts HandlerOptions) *Handler {
	if opts.MaxUploadBytes <= 0 {
		opts.MaxUploadBytes = 10 << 20
	}
	return &Handler{svc: svc, opt: opts}
}

// Upload handles POST /api/v1/files (multipart/form-data).
func (h *Handler) Upload(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	// ParseMultipartForm keeps the parts in memory up to maxMemory. The body
	// limit already prevents anything beyond max_upload_bytes + overhead from
	// being read, so there is no disk spill and the memory budget is bounded.
	if err := c.Request.ParseMultipartForm(h.opt.MaxUploadBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.Fail(c, apperr.Wrap(apperr.CodePayloadTooLarge, "request body too large", err))
			return
		}
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, "invalid multipart form", err))
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	header, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, "file part is required", err))
		return
	}
	body, err := header.Open()
	if err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, "open upload failed", err))
		return
	}
	defer func() { _ = body.Close() }()

	in := UploadInput{
		Filename: header.Filename,
		Size:     header.Size,
		Body:     body,
		BizType:  c.PostForm("biz_type"),
	}
	if raw := c.PostForm("visibility"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, "invalid visibility", err))
			return
		}
		visibility := Visibility(value)
		if !visibility.Valid() {
			httpx.Fail(c, apperr.New(apperr.CodeInvalidArgument, "invalid visibility"))
			return
		}
		in.Visibility = &visibility
	}

	record, err := h.svc.Upload(c.Request.Context(), actor, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, toStoredFileDTO(record))
}

// List handles GET /api/v1/files.
func (h *Handler) List(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	files, total, err := h.svc.List(c.Request.Context(), actor, ListQuery{
		Page:     page,
		PageSize: pageSize,
		BizType:  c.Query("biz_type"),
		Keyword:  c.Query("keyword"),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, pageSize = normalizePagination(page, pageSize)
	httpx.OK(c, FileListResponse{
		Items:    toStoredFileDTOs(files),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// Get handles GET /api/v1/files/:id.
func (h *Handler) Get(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	record, err := h.svc.Get(c.Request.Context(), actor, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toStoredFileDTO(record))
}

// URL handles GET /api/v1/files/:id/url: a short-lived signed download URL.
func (h *Handler) URL(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var ttl time.Duration
	if raw := c.Query("ttl_seconds"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			httpx.Fail(c, apperr.New(apperr.CodeInvalidArgument, "invalid ttl_seconds"))
			return
		}
		ttl = time.Duration(seconds) * time.Second
	}
	result, err := h.svc.URL(c.Request.Context(), actor, id, c.Query("disposition"), ttl)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, URLResponse(result))
}

// UpdateVisibility handles PATCH /api/v1/files/:id.
func (h *Handler) UpdateVisibility(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req UpdateVisibilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	record, err := h.svc.UpdateVisibility(c.Request.Context(), actor, id, req.Visibility)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toStoredFileDTO(record))
}

// Delete handles DELETE /api/v1/files/:id (soft delete + async blob purge).
func (h *Handler) Delete(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), actor, id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// requirePrincipal reads the authenticated caller injected by authkit. Missing
// means the route was mounted without Authenticate: fail closed with 401.
func requirePrincipal(c *gin.Context) (authkit.Principal, bool) {
	principal, ok := authkit.PrincipalFrom(c)
	if !ok {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "authentication required"))
		return authkit.Principal{}, false
	}
	return principal, true
}

func parseID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, apperr.Wrap(apperr.CodeInvalidArgument, "invalid id", err)
	}
	return id, nil
}

// validationMessage converts gin binding errors into a readable client message.
func validationMessage(err error) string {
	var ve interface{ Error() string }
	if errors.As(err, &ve) {
		return ve.Error()
	}
	return "invalid request"
}
