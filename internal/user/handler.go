package user

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// Handler exposes the user use-cases over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler creates a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Create handles POST /api/v1/users.
func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	created, err := h.svc.Create(c.Request.Context(), Input(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, toDTO(created))
}

// Get handles GET /api/v1/users/:id.
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
	found, err := h.svc.Get(c.Request.Context(), actor, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toDTO(found))
}

// List handles GET /api/v1/users.
func (h *Handler) List(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")

	users, total, err := h.svc.List(c.Request.Context(), actor, page, pageSize, keyword)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, ListResponse{
		Items:    toDTOs(users),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// Update handles PATCH /api/v1/users/:id.
func (h *Handler) Update(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	updated, err := h.svc.Update(c.Request.Context(), actor, id, Patch(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toDTO(updated))
}

// Delete handles DELETE /api/v1/users/:id.
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
// means the route was mounted without Authenticate, which is a wiring bug: fail
// closed with 401 rather than serving anonymously.
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
		return 0, apperr.Wrap(apperr.CodeInvalidArgument, "invalid user id", err)
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
