package user

import (
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// RegisterRoutes mounts the user HTTP API on the given group. The group is
// expected to be the authenticated /api/v1 group: every operation here needs a
// valid access token and additionally checks its own permission.
func RegisterRoutes(rg *routekit.Group, h *Handler) {
	rg.Registry().DescribeTag("users", "User management (authenticated, RBAC)")
	users := rg.Group("/users", routekit.Tags("users"), routekit.Security(routekit.BearerAuth))

	// Per-permission sub-groups: routekit applies the middleware to every route
	// registered on the child group, so the guard and the documented security
	// requirement cannot drift apart.
	readAny := users.Group("", routekit.Middleware(
		authkit.RequirePermission(authkit.PermUsersRead, authkit.PermUsersReadSelf),
	))
	writeAny := users.Group("", routekit.Middleware(
		authkit.RequirePermission(authkit.PermUsersWrite, authkit.PermUsersWriteSelf),
	))
	adminWrite := users.Group("", routekit.Middleware(
		authkit.RequirePermission(authkit.PermUsersWrite),
	))
	adminDelete := users.Group("", routekit.Middleware(
		authkit.RequirePermission(authkit.PermUsersDelete),
	))

	adminWrite.POST("", &routekit.Op{
		ID:        "createUser",
		Summary:   "Create a user (admin)",
		Request:   routekit.JSON[CreateRequest](true),
		Responses: []routekit.Response{routekit.Created[UserDTO]("created user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.Create)

	adminWrite.GET("", &routekit.Op{
		ID:      "listUsers",
		Summary: "List users with pagination and keyword filter (admin)",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[string]("keyword", "Keyword matched against name/email", false),
		},
		Responses: []routekit.Response{routekit.OK[ListResponse]("user page")},
		Errors:    []apperr.Code{apperr.CodeForbidden},
	}, h.List)

	readAny.GET("/:id", &routekit.Op{
		ID:        "getUser",
		Summary:   "Get a user by id (admin, or self)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "User ID")},
		Responses: []routekit.Response{routekit.OK[UserDTO]("user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.Get)

	writeAny.PATCH("/:id", &routekit.Op{
		ID:      "updateUser",
		Summary: "Update a user (admin any field, member only own name/phone)",
		Description: "Members may patch only their own name and phone. Role, status, password and email are admin-only: " +
			"the email conflict error would otherwise let any member test whether an address is registered.",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "User ID")},
		Request:   routekit.JSON[UpdateRequest](true),
		Responses: []routekit.Response{routekit.OK[UserDTO]("user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeConflict, apperr.CodeForbidden},
	}, h.Update)

	adminDelete.DELETE("/:id", &routekit.Op{
		ID:        "deleteUser",
		Summary:   "Delete a user (soft delete, admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "User ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.Delete)
}
