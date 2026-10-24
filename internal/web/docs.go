package web

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/example/go-web-template/internal/platform/routekit"
)

// MountDocs mounts the generated OpenAPI document and, when enabled is true,
// the Swagger UI. It must be called after every domain has registered its
// routes so the document is complete. The document is validated even when the
// UI is disabled, so invalid metadata still fails startup.
func MountDocs(engine *gin.Engine, api *routekit.Group, enabled bool) error {
	if api == nil {
		return fmt.Errorf("web: MountDocs requires a non-nil API group")
	}
	return routekit.ServeDocs(engine, api.Registry(), "/openapi.json", enabled)
}
