package user

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
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

// CreateRequest is the payload for POST /users.
type CreateRequest struct {
	Name  string `json:"name" binding:"required,min=2,max=64"`
	Email string `json:"email" binding:"required,email,max=255"`
}

// UpdateRequest is the payload for PATCH /users/:id.
type UpdateRequest struct {
	Name  *string `json:"name" binding:"omitempty,min=2,max=64"`
	Email *string `json:"email" binding:"omitempty,email,max=255"`
}

// Create handles POST /api/v1/users.
//
//	@Summary	Create a user
//	@Tags		users
//	@Accept		json
//	@Produce	json
//	@Param		body	body		CreateRequest	true	"User payload"
//	@Success	201		{object}	httpx.Body
//	@Failure	400		{object}	httpx.Body
//	@Failure	409		{object}	httpx.Body
//	@Router		/users [post]
func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	user, err := h.svc.Create(c.Request.Context(), Input(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, user)
}

// Get handles GET /api/v1/users/:id.
//
//	@Summary	Get a user by id
//	@Tags		users
//	@Produce	json
//	@Param		id	path		int	true	"User ID"
//	@Success	200	{object}	httpx.Body
//	@Failure	404	{object}	httpx.Body
//	@Router		/users/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	user, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, user)
}

// List handles GET /api/v1/users.
//
//	@Summary	List users with pagination and keyword filter
//	@Tags		users
//	@Produce	json
//	@Param		page		query	int	false	"Page number (default 1)"	mininum(1)
//	@Param		page_size	query	int	false	"Page size (default 20, max 100)"	maximum(100)
//	@Param		keyword		query	string	false	"Keyword matched against name/email"
//	@Success	200			{object}	httpx.Body
//	@Router		/users [get]
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")

	users, total, err := h.svc.List(c.Request.Context(), page, pageSize, keyword)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{
		"items":     users,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Update handles PATCH /api/v1/users/:id.
//
//	@Summary	Update a user
//	@Tags		users
//	@Accept		json
//	@Produce	json
//	@Param		id		path	int					true	"User ID"
//	@Param		body	body	UpdateRequest	true	"Fields to update"
//	@Success	200		{object}	httpx.Body
//	@Failure	400		{object}	httpx.Body
//	@Failure	404		{object}	httpx.Body
//	@Failure	409		{object}	httpx.Body
//	@Router		/users/{id} [patch]
func (h *Handler) Update(c *gin.Context) {
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
	user, err := h.svc.Update(c.Request.Context(), id, Patch(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, user)
}

// Delete handles DELETE /api/v1/users/:id.
//
//	@Summary	Delete a user (soft delete)
//	@Tags		users
//	@Produce	json
//	@Param		id	path	int	true	"User ID"
//	@Success	204	"no content"
//	@Failure	404	{object}	httpx.Body
//	@Router		/users/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
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
