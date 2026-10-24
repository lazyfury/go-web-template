package dict

import (
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// RegisterRoutes mounts the dictionary API on the authenticated /api/v1 group.
// Reads are available to every role (dicts:read); mutations require
// dicts:write (admin).
func RegisterRoutes(rg *routekit.Group, h *Handler) {
	rg.Registry().DescribeTag("dicts", "Dictionaries and settings (group = setting, items = dict array)")

	read := rg.Group("/dicts",
		routekit.Tags("dicts"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermDictsRead)),
	)
	read.GET("/:group", &routekit.Op{
		ID:      "getDictValues",
		Summary: "Get the enabled dict array of a group",
		Params:  []routekit.Param{routekit.Path[string]("group", "Group key, e.g. user.status")},
		Responses: []routekit.Response{
			routekit.OK[[]ItemDTO]("dict array ordered by sort"),
		},
		Errors: []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.GetValues)

	admin := rg.Group("",
		routekit.Tags("dicts"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermDictsWrite)),
	)

	admin.PUT("/dicts/:group", &routekit.Op{
		ID:        "replaceDictValues",
		Summary:   "Replace a whole setting with an object or array value (admin)",
		Params:    []routekit.Param{routekit.Path[string]("group", "Group key, e.g. site.seo")},
		Request:   routekit.JSON[ReplaceValuesRequest](true),
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeForbidden},
	}, h.ReplaceValues)

	admin.POST("/dict-groups", &routekit.Op{
		ID:        "createDictGroup",
		Summary:   "Create a dict group (admin)",
		Request:   routekit.JSON[CreateGroupRequest](true),
		Responses: []routekit.Response{routekit.Created[GroupDTO]("created group")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.CreateGroup)

	admin.GET("/dict-groups", &routekit.Op{
		ID:      "listDictGroups",
		Summary: "List dict groups with pagination and keyword filter (admin)",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[string]("keyword", "Keyword matched against key/name", false),
		},
		Responses: []routekit.Response{routekit.OK[GroupListResponse]("group page")},
		Errors:    []apperr.Code{apperr.CodeForbidden},
	}, h.ListGroups)

	admin.GET("/dict-groups/:id", &routekit.Op{
		ID:        "getDictGroup",
		Summary:   "Get a dict group by id (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Group ID")},
		Responses: []routekit.Response{routekit.OK[GroupDTO]("group")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.GetGroup)

	admin.PATCH("/dict-groups/:id", &routekit.Op{
		ID:        "updateDictGroup",
		Summary:   "Update a dict group's name/description/status (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Group ID")},
		Request:   routekit.JSON[UpdateGroupRequest](true),
		Responses: []routekit.Response{routekit.OK[GroupDTO]("updated group")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.UpdateGroup)

	admin.DELETE("/dict-groups/:id", &routekit.Op{
		ID:        "deleteDictGroup",
		Summary:   "Delete a dict group and its items (soft delete, admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Group ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.DeleteGroup)

	admin.GET("/dict-groups/:id/items", &routekit.Op{
		ID:      "listDictItems",
		Summary: "List all items of a group, including disabled ones (admin)",
		Params: []routekit.Param{
			routekit.Path[uint64]("id", "Group ID"),
			routekit.Query[bool]("include_disabled", "Include disabled items (default false)", false),
		},
		Responses: []routekit.Response{routekit.OK[[]ItemDTO]("group items")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.ListItems)

	admin.POST("/dict-groups/:id/items", &routekit.Op{
		ID:        "createDictItem",
		Summary:   "Create an item in a group (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Group ID")},
		Request:   routekit.JSON[CreateItemRequest](true),
		Responses: []routekit.Response{routekit.Created[ItemDTO]("created item")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.CreateItem)

	admin.PATCH("/dict-items/:id", &routekit.Op{
		ID:        "updateDictItem",
		Summary:   "Update a dict item (admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Item ID")},
		Request:   routekit.JSON[UpdateItemRequest](true),
		Responses: []routekit.Response{routekit.OK[ItemDTO]("updated item")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.UpdateItem)

	admin.DELETE("/dict-items/:id", &routekit.Op{
		ID:        "deleteDictItem",
		Summary:   "Delete a dict item (soft delete, admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "Item ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.DeleteItem)
}
