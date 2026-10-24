package user

import (
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// RegisterRoutes mounts the user HTTP API on the given group. The group is
// expected to carry the /api/v1 middleware chain (rate limit, request
// timeout); this module only owns its own paths and route metadata.
func RegisterRoutes(rg *routekit.Group, h *Handler) {
	rg.Registry().DescribeTag("users", "User management")
	users := rg.Group("/users", routekit.Tags("users"))

	users.POST("", &routekit.Op{
		ID:        "createUser",
		Summary:   "Create a user",
		Request:   routekit.JSON[CreateRequest](true),
		Responses: []routekit.Response{routekit.Created[UserDTO]("created user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeConflict},
	}, h.Create)

	users.GET("", &routekit.Op{
		ID:      "listUsers",
		Summary: "List users with pagination and keyword filter",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[string]("keyword", "Keyword matched against name/email", false),
		},
		Responses: []routekit.Response{routekit.OK[ListResponse]("user page")},
	}, h.List)

	users.GET("/:id", &routekit.Op{
		ID:        "getUser",
		Summary:   "Get a user by id",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "User ID")},
		Responses: []routekit.Response{routekit.OK[UserDTO]("user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound},
	}, h.Get)

	users.PATCH("/:id", &routekit.Op{
		ID:        "updateUser",
		Summary:   "Update a user",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "User ID")},
		Request:   routekit.JSON[UpdateRequest](true),
		Responses: []routekit.Response{routekit.OK[UserDTO]("user")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeConflict},
	}, h.Update)

	users.DELETE("/:id", &routekit.Op{
		ID:        "deleteUser",
		Summary:   "Delete a user (soft delete)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "User ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound},
	}, h.Delete)
}
