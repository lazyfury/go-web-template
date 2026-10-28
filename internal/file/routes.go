package file

import (
	"reflect"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/authkit"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// RegisterRoutes mounts the file API on the authenticated /api/v1 group.
//
// files:read covers metadata and signed URLs (members only see their own rows,
// enforced in the service); files:write covers upload/mutate/delete.
func RegisterRoutes(rg *routekit.Group, h *Handler) {
	rg.Registry().DescribeTag("files", "File upload, metadata and signed download URLs")

	read := rg.Group("/files",
		routekit.Tags("files"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermFilesRead)),
	)

	read.GET("", &routekit.Op{
		ID:      "listFiles",
		Summary: "List files (members: own files, admins: all files)",
		Params: []routekit.Param{
			routekit.Query[int]("page", "Page number (default 1)", false),
			routekit.Query[int]("page_size", "Page size (default 20, max 100)", false),
			routekit.Query[string]("biz_type", "Filter by business type", false),
			routekit.Query[string]("keyword", "Keyword matched against the original file name", false),
		},
		Responses: []routekit.Response{routekit.OK[FileListResponse]("file page")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeForbidden},
	}, h.List)

	read.GET("/:id", &routekit.Op{
		ID:        "getFile",
		Summary:   "Get file metadata",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "File ID")},
		Responses: []routekit.Response{routekit.OK[StoredFileDTO]("file metadata")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.Get)

	read.GET("/:id/url", &routekit.Op{
		ID:      "getFileURL",
		Summary: "Get a short-lived signed download URL",
		Params: []routekit.Param{
			routekit.Path[uint64]("id", "File ID"),
			routekit.Query[string]("disposition", "inline or attachment (inline only for whitelisted types)", false),
			routekit.Query[int]("ttl_seconds", "URL lifetime in seconds (default 600, max 3600)", false),
		},
		Responses: []routekit.Response{routekit.OK[URLResponse]("signed download URL")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.URL)

	write := rg.Group("",
		routekit.Tags("files"),
		routekit.Security(routekit.BearerAuth),
		routekit.Middleware(authkit.RequirePermission(authkit.PermFilesWrite)),
	)

	write.POST("/files", &routekit.Op{
		ID:      "uploadFile",
		Summary: "Upload one file (multipart/form-data)",
		Description: "The `file` part carries the bytes; `biz_type` and `visibility` are optional text fields. " +
			"The content type is sniffed server-side and must be on storage.allowed_mime; " +
			"the size limit is storage.max_upload_bytes (default 10 MiB).",
		Request: &routekit.RequestBody{
			Type:        reflect.TypeFor[UploadRequest](),
			Required:    true,
			ContentType: "multipart/form-data",
			Description: "One multipart part named file plus optional biz_type/visibility fields.",
		},
		Responses: []routekit.Response{routekit.Created[StoredFileDTO]("stored file")},
		Errors: []apperr.Code{
			apperr.CodeInvalidArgument, apperr.CodePayloadTooLarge, apperr.CodeForbidden,
		},
	}, h.Upload)

	write.PATCH("/files/:id", &routekit.Op{
		ID:        "updateFileVisibility",
		Summary:   "Change a file's visibility (owner or admin)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "File ID")},
		Request:   routekit.JSON[UpdateVisibilityRequest](true),
		Responses: []routekit.Response{routekit.OK[StoredFileDTO]("updated file")},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.UpdateVisibility)

	write.DELETE("/files/:id", &routekit.Op{
		ID:        "deleteFile",
		Summary:   "Delete a file (soft delete, blob purged asynchronously)",
		Params:    []routekit.Param{routekit.Path[uint64]("id", "File ID")},
		Responses: []routekit.Response{routekit.NoContent()},
		Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound, apperr.CodeForbidden},
	}, h.Delete)
}
