package dict

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// Handler exposes the dictionary use-cases over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler creates a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetValues handles GET /api/v1/dicts/:group: the consumer-facing dict array.
func (h *Handler) GetValues(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	items, err := h.svc.Get(c.Request.Context(), c.Param("group"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toItemDTOs(items))
}

// ReplaceValues handles PUT /api/v1/dicts/:group: whole-setting replacement
// using the same object/array semantics as dict.Store.Replace.
func (h *Handler) ReplaceValues(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	var req ReplaceValuesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	if err := h.svc.Replace(c.Request.Context(), c.Param("group"), req.Value); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// CreateGroup handles POST /api/v1/dict-groups.
func (h *Handler) CreateGroup(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	status := StatusEnabled
	if req.Status != nil {
		status = *req.Status
	}
	group, err := h.svc.CreateGroup(c.Request.Context(), CreateGroupInput{
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Status:      status,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, toGroupDTO(group, 0))
}

// GetGroup handles GET /api/v1/dict-groups/:id.
func (h *Handler) GetGroup(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	group, itemCount, err := h.svc.GetGroup(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toGroupDTO(group, itemCount))
}

// ListGroups handles GET /api/v1/dict-groups.
func (h *Handler) ListGroups(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = normalizePagination(page, pageSize)

	groups, counts, total, err := h.svc.ListGroups(c.Request.Context(), page, pageSize, c.Query("keyword"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]GroupDTO, 0, len(groups))
	for i := range groups {
		items = append(items, toGroupDTO(&groups[i], counts[groups[i].ID]))
	}
	httpx.OK(c, GroupListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// UpdateGroup handles PATCH /api/v1/dict-groups/:id.
func (h *Handler) UpdateGroup(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	group, err := h.svc.UpdateGroup(c.Request.Context(), id, CreateGroupPatch(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	_, itemCount, err := h.svc.GetGroup(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toGroupDTO(group, itemCount))
}

// DeleteGroup handles DELETE /api/v1/dict-groups/:id.
func (h *Handler) DeleteGroup(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.DeleteGroup(c.Request.Context(), id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// ListItems handles GET /api/v1/dict-groups/:id/items.
func (h *Handler) ListItems(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	includeDisabled, err := parseBoolQuery(c, "include_disabled")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, err := h.svc.ListItems(c.Request.Context(), id, includeDisabled)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toItemDTOs(items))
}

// CreateItem handles POST /api/v1/dict-groups/:id/items.
func (h *Handler) CreateItem(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req CreateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	status := StatusEnabled
	if req.Status != nil {
		status = *req.Status
	}
	item, err := h.svc.CreateItem(c.Request.Context(), id, &CreateItemInput{
		Key:       req.Key,
		Label:     req.Label,
		Value:     req.Value,
		ValueType: req.ValueType,
		SortOrder: req.SortOrder,
		Status:    status,
		Remark:    req.Remark,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, toItemDTO(item))
}

// UpdateItem handles PATCH /api/v1/dict-items/:id.
func (h *Handler) UpdateItem(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req UpdateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	item, err := h.svc.UpdateItem(c.Request.Context(), id, CreateItemPatch(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toItemDTO(item))
}

// DeleteItem handles DELETE /api/v1/dict-items/:id.
func (h *Handler) DeleteItem(c *gin.Context) {
	if !requirePrincipal(c) {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.DeleteItem(c.Request.Context(), id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// requirePrincipal reads the authenticated caller injected by authkit. Missing
// means the route was mounted without Authenticate, which is a wiring bug: fail
// closed with 401 rather than serving anonymously.
func requirePrincipal(c *gin.Context) bool {
	if _, ok := authkit.PrincipalFrom(c); !ok {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthorized, "authentication required"))
		return false
	}
	return true
}

func parseID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, apperr.Wrap(apperr.CodeInvalidArgument, "invalid id", err)
	}
	return id, nil
}

func parseBoolQuery(c *gin.Context, name string) (bool, error) {
	raw := c.Query(name)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, apperr.Wrap(apperr.CodeInvalidArgument, "invalid "+name+" value", err)
	}
	return value, nil
}

// validationMessage converts gin binding errors into a readable client message.
func validationMessage(err error) string {
	var ve interface{ Error() string }
	if errors.As(err, &ve) {
		return ve.Error()
	}
	return "invalid request"
}
