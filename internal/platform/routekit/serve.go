package routekit

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// ServeDocs mounts the generated OpenAPI document at specPath and the Swagger
// UI at /swagger/*any when enabled is true. The document is always built and
// validated so invalid metadata fails startup even when the docs are disabled.
func ServeDocs(engine *gin.Engine, reg *Registry, specPath string, enabled bool) error {
	if engine == nil {
		return fmt.Errorf("routekit: ServeDocs requires a non-nil engine")
	}
	if reg == nil {
		return fmt.Errorf("routekit: ServeDocs requires a non-nil registry")
	}
	if specPath == "" {
		specPath = "/openapi.json"
	}

	data, err := reg.JSON()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	engine.GET(specPath, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	})

	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler, ginSwagger.URL(specPath)))
	return nil
}
