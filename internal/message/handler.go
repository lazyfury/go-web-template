package message

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/httpx"
)

// Handler exposes the message use-cases over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler creates a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Send handles POST /api/v1/messages.
func (h *Handler) Send(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	var req SendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	result, err := h.svc.Send(c.Request.Context(), actor, &SendInput{
		Title:       req.Title,
		Body:        req.Body,
		Type:        req.Type,
		Level:       req.Level,
		Link:        req.Link,
		Channels:    req.Channels,
		Recipients:  RecipientFilter{All: req.Recipients.All, UserIDs: req.Recipients.UserIDs, Roles: req.Recipients.Roles},
		TemplateKey: req.TemplateKey,
		Locale:      req.Locale,
		Data:        req.Data,
		ExpiresAt:   req.ExpiresAt,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, SendResultDTO{
		MessageID:      result.MessageID,
		RecipientCount: result.RecipientCount,
		DeliveryCount:  result.DeliveryCount,
	})
}

// Inbox handles GET /api/v1/messages/inbox.
func (h *Handler) Inbox(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	unreadOnly, err := parseBoolQuery(c, "unread_only")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, total, page, pageSize, err := h.svc.Inbox(c.Request.Context(), actor, page, pageSize, unreadOnly)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, InboxResponse{
		Items:    toInboxDTOs(items),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// UnreadCount handles GET /api/v1/messages/unread-count.
func (h *Handler) UnreadCount(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	count, err := h.svc.UnreadCount(c.Request.Context(), actor)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, UnreadCountResponse{Count: count})
}

// GetInbox handles GET /api/v1/messages/inbox/:id.
func (h *Handler) GetInbox(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.GetInboxMessage(c.Request.Context(), actor, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toInboxDTO(item))
}

// MarkRead handles PATCH /api/v1/messages/inbox/:id/read.
func (h *Handler) MarkRead(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), actor, id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// MarkAllRead handles POST /api/v1/messages/read-all.
func (h *Handler) MarkAllRead(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	if err := h.svc.MarkAllRead(c.Request.Context(), actor); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// DeleteInbox handles DELETE /api/v1/messages/inbox/:id.
func (h *Handler) DeleteInbox(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.DeleteInbox(c.Request.Context(), actor, id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// ListSent handles GET /api/v1/messages/sent.
func (h *Handler) ListSent(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, page, pageSize, err := h.svc.ListSent(c.Request.Context(), actor, page, pageSize)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, SentListResponse{
		Items:    toSentDTOs(items),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// CreateTemplate handles POST /api/v1/message-templates.
func (h *Handler) CreateTemplate(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	var req CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	status := StatusEnabled
	if req.Status != nil {
		status = *req.Status
	}
	template, err := h.svc.CreateTemplate(c.Request.Context(), actor, &TemplateInput{
		Key:       req.Key,
		Channel:   req.Channel,
		Locale:    req.Locale,
		Subject:   req.Subject,
		Body:      req.Body,
		Variables: req.Variables,
		Status:    status,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, toTemplateDTO(template))
}

// ListTemplates handles GET /api/v1/message-templates.
func (h *Handler) ListTemplates(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	channel, err := parseChannelFilter(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status, err := parseTemplateStatusFilter(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, total, page, pageSize, err := h.svc.ListTemplates(c.Request.Context(), actor, page, pageSize, channel, status)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, TemplateListResponse{
		Items:    toTemplateDTOs(items),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// GetTemplate handles GET /api/v1/message-templates/:id.
func (h *Handler) GetTemplate(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	template, err := h.svc.GetTemplate(c.Request.Context(), actor, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toTemplateDTO(template))
}

// UpdateTemplate handles PATCH /api/v1/message-templates/:id.
func (h *Handler) UpdateTemplate(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req UpdateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err))
		return
	}
	template, err := h.svc.UpdateTemplate(c.Request.Context(), actor, id, TemplatePatch(req))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, toTemplateDTO(template))
}

// DeleteTemplate handles DELETE /api/v1/message-templates/:id.
func (h *Handler) DeleteTemplate(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.DeleteTemplate(c.Request.Context(), actor, id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// ListDeliveries handles GET /api/v1/message-deliveries.
func (h *Handler) ListDeliveries(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	channel, err := parseChannelFilter(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status, err := parseDeliveryStatusFilter(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, total, page, pageSize, err := h.svc.ListDeliveries(c.Request.Context(), actor, page, pageSize, status, channel)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, DeliveryListResponse{
		Items:    toDeliveryDTOs(items),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// RetryDelivery handles POST /api/v1/message-deliveries/:id/retry.
func (h *Handler) RetryDelivery(c *gin.Context) {
	actor, ok := requirePrincipal(c)
	if !ok {
		return
	}
	id, err := parseID(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.RetryDelivery(c.Request.Context(), actor, id); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

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

func parseChannelFilter(c *gin.Context) (*Channel, error) {
	raw := c.Query("channel")
	if raw == "" {
		return nil, nil
	}
	channel := Channel(raw)
	if !channel.Valid() {
		return nil, invalid("invalid channel filter")
	}
	return &channel, nil
}

func parseTemplateStatusFilter(c *gin.Context) (*Status, error) {
	raw := c.Query("status")
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, invalid("invalid status filter")
	}
	status := Status(value)
	if !status.Valid() {
		return nil, invalid("invalid status filter")
	}
	return &status, nil
}

func parseDeliveryStatusFilter(c *gin.Context) (*DeliveryStatus, error) {
	raw := c.Query("status")
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, invalid("invalid delivery status filter")
	}
	status := DeliveryStatus(value)
	if !status.Valid() {
		return nil, invalid("invalid delivery status filter")
	}
	return &status, nil
}

func validationMessage(err error) string {
	var ve interface{ Error() string }
	if errors.As(err, &ve) {
		return ve.Error()
	}
	return "invalid request"
}
