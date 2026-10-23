package handler

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	apperrors "github.com/example/go-web-template/internal/errors"
	"github.com/example/go-web-template/internal/response"
	"github.com/example/go-web-template/internal/service"
)

// UserHandler exposes the user use-cases over HTTP.
type UserHandler struct {
	service service.UserService
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{service: svc}
}

// CreateUserRequest is the payload for POST /users.
type CreateUserRequest struct {
	Name  string `json:"name" binding:"required,min=2,max=64"`
	Email string `json:"email" binding:"required,email,max=255"`
}

// UpdateUserRequest is the payload for PATCH /users/:id.
type UpdateUserRequest struct {
	Name  *string `json:"name" binding:"omitempty,min=2,max=64"`
	Email *string `json:"email" binding:"omitempty,email,max=255"`
}

// Create handles POST /api/v1/users.
//
//	@Summary	Create a user
//	@Tags		users
//	@Accept		json
//	@Produce	json
//	@Param		body	body		CreateUserRequest	true	"User payload"
//	@Success	201		{object}	response.Body
//	@Failure	400		{object}	response.Body
//	@Failure	409		{object}	response.Body
//	@Router		/users [post]
func (h *UserHandler) Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperrors.Wrap(apperrors.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	user, err := h.service.Create(c.Request.Context(), service.UserInput{
		Name:  req.Name,
		Email: req.Email,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, user)
}

// Get handles GET /api/v1/users/:id.
//
//	@Summary	Get a user by id
//	@Tags		users
//	@Produce	json
//	@Param		id	path		int	true	"User ID"
//	@Success	200	{object}	response.Body
//	@Failure	404	{object}	response.Body
//	@Router		/users/{id} [get]
func (h *UserHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	user, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, user)
}

// List handles GET /api/v1/users.
//
//	@Summary	List users with pagination and keyword filter
//	@Tags		users
//	@Produce	json
//	@Param		page		query	int	false	"Page number (default 1)"	mininum(1)
//	@Param		page_size	query	int	false	"Page size (default 20, max 100)"	maximum(100)
//	@Param		keyword		query	string	false	"Keyword matched against name/email"
//	@Success	200			{object}	response.Body
//	@Router		/users [get]
func (h *UserHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")

	users, total, err := h.service.List(c.Request.Context(), page, pageSize, keyword)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
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
//	@Param		body	body	UpdateUserRequest	true	"Fields to update"
//	@Success	200		{object}	response.Body
//	@Failure	400		{object}	response.Body
//	@Failure	404		{object}	response.Body
//	@Failure	409		{object}	response.Body
//	@Router		/users/{id} [patch]
func (h *UserHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperrors.Wrap(apperrors.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	user, err := h.service.Update(c.Request.Context(), id, service.UserPatch{
		Name:  req.Name,
		Email: req.Email,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, user)
}

// Delete handles DELETE /api/v1/users/:id.
//
//	@Summary	Delete a user (soft delete)
//	@Tags		users
//	@Produce	json
//	@Param		id	path	int	true	"User ID"
//	@Success	204	"no content"
//	@Failure	404	{object}	response.Body
//	@Router		/users/{id} [delete]
func (h *UserHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

func parseID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, apperrors.Wrap(apperrors.CodeInvalidArgument, "invalid user id", err)
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
