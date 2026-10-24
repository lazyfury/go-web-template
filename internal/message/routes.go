package message

import (
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// RegisterRoutes mounts the message API on the authenticated /api/v1 group.
// Inbox reads are available to every role; sending/templates/deliveries are
// admin-only permissions.
func RegisterRoutes(rg *routekit.Group, h *Handler) {
	rg.Registry().DescribeTag("messages", "In-app inbox and multi-channel notification delivery")

	read := rg.Group("/messages",
		routekit.Tags("messages"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermMessagesRead)),
	)
	read.GET("/inbox", &routekit.Op{
		ID:      "listInbox",
		Summary: "List the caller's inbox",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[bool]("unread_only", "Only unread messages (default false)", false),
		},
		Responses: []routekit.Response{routekit.OK[InboxResponse]("inbox page")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeForbidden},
	}, h.Inbox)

	read.GET("/unread-count", &routekit.Op{
		ID:        "getUnreadCount",
		Summary:   "Get the caller's unread message count",
		Responses: []routekit.Response{routekit.OK[UnreadCountResponse]("unread count")},
		Errors:    []apperr.Code{apperr.CodeForbidden},
	}, h.UnreadCount)

	read.GET("/inbox/:id", &routekit.Op{
		ID:        "getInboxMessage",
		Summary:   "Get one inbox message",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Message ID")},
		Responses: []routekit.Response{routekit.OK[InboxItemDTO]("inbox message")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.GetInbox)

	read.PATCH("/inbox/:id/read", &routekit.Op{
		ID:        "markMessageRead",
		Summary:   "Mark one inbox message as read",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Message ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.MarkRead)

	read.POST("/read-all", &routekit.Op{
		ID:        "markAllMessagesRead",
		Summary:   "Mark every inbox message as read",
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeForbidden},
	}, h.MarkAllRead)

	read.DELETE("/inbox/:id", &routekit.Op{
		ID:        "deleteInboxMessage",
		Summary:   "Delete one inbox message for the caller",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Message ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.DeleteInbox)

	send := rg.Group("",
		routekit.Tags("messages"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermMessagesSend)),
	)
	send.POST("/messages", &routekit.Op{
		ID:        "sendMessage",
		Summary:   "Send a message to users, all users or a role (admin)",
		Request:   routekit.JSON[SendRequest](true),
		Responses: []routekit.Response{routekit.Created[SendResultDTO]("message created")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeForbidden},
	}, h.Send)

	send.GET("/messages/sent", &routekit.Op{
		ID:      "listSentMessages",
		Summary: "List sent messages with read statistics (admin)",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
		},
		Responses: []routekit.Response{routekit.OK[SentListResponse]("sent messages")},
		Errors:    []apperr.Code{apperr.CodeForbidden},
	}, h.ListSent)

	manage := rg.Group("",
		routekit.Tags("messages"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermMessagesManage)),
	)
	manage.GET("/message-templates", &routekit.Op{
		ID:      "listMessageTemplates",
		Summary: "List message templates (admin)",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[string]("channel", "Filter by channel", false),
			routekit.Query[int]("status", "Filter by status (0/1)", false),
		},
		Responses: []routekit.Response{routekit.OK[TemplateListResponse]("template page")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeForbidden},
	}, h.ListTemplates)

	manage.POST("/message-templates", &routekit.Op{
		ID:        "createMessageTemplate",
		Summary:   "Create a message template (admin)",
		Request:   routekit.JSON[CreateTemplateRequest](true),
		Responses: []routekit.Response{routekit.Created[TemplateDTO]("created template")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.CreateTemplate)

	manage.GET("/message-templates/:id", &routekit.Op{
		ID:        "getMessageTemplate",
		Summary:   "Get a message template (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Template ID")},
		Responses: []routekit.Response{routekit.OK[TemplateDTO]("template")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.GetTemplate)

	manage.PATCH("/message-templates/:id", &routekit.Op{
		ID:        "updateMessageTemplate",
		Summary:   "Update a message template (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Template ID")},
		Request:   routekit.JSON[UpdateTemplateRequest](true),
		Responses: []routekit.Response{routekit.OK[TemplateDTO]("updated template")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.UpdateTemplate)

	manage.DELETE("/message-templates/:id", &routekit.Op{
		ID:        "deleteMessageTemplate",
		Summary:   "Delete a message template (soft delete, admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Template ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.DeleteTemplate)

	manage.GET("/message-deliveries", &routekit.Op{
		ID:      "listMessageDeliveries",
		Summary: "List external delivery records (admin)",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[int]("status", "Filter by delivery status (0-5)", false),
			routekit.Query[string]("channel", "Filter by channel", false),
		},
		Responses: []routekit.Response{routekit.OK[DeliveryListResponse]("delivery page")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeForbidden},
	}, h.ListDeliveries)

	manage.POST("/message-deliveries/:id/retry", &routekit.Op{
		ID:        "retryMessageDelivery",
		Summary:   "Retry a failed or dead delivery (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Delivery ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.RetryDelivery)
}
