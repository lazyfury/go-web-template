// Package router wires the HTTP routes and middleware chain.
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/config"
	"github.com/example/go-web-template/internal/handler"
	"github.com/example/go-web-template/internal/middleware"

	// Registers the generated OpenAPI spec (docs/swagger.json) with gin-swagger.
	_ "github.com/example/go-web-template/docs"
)

// Handlers bundles all HTTP handlers passed to the router.
type Handlers struct {
	User   *handler.UserHandler
	Health *handler.HealthHandler
}

// New builds the gin engine with the full middleware chain and route table.
func New(cfg *config.Config, logger *zap.Logger, h Handlers) *gin.Engine {
	if cfg.App.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// Global middleware. Recovery must run first so it can catch panics from
	// every other middleware; RequestID before Logger so logs carry the id.
	r.Use(
		middleware.Recovery(logger),
		middleware.RequestID(),
		middleware.Logger(logger),
		middleware.CORS(&cfg.CORS),
		middleware.Metrics(),
	)

	// Ops endpoints: liveness, readiness and Prometheus metrics. These are
	// intentionally not rate-limited so orchestrators can always probe.
	r.GET("/healthz", h.Health.Liveness)
	r.GET("/readyz", h.Health.Readiness)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if cfg.Swagger.Enabled {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	}

	// API v1.
	v1 := r.Group("/api/v1", middleware.RateLimit(cfg.RateLimit.Enabled, cfg.RateLimit.RPS, cfg.RateLimit.Burst))
	users := v1.Group("/users")
	users.POST("", h.User.Create)
	users.GET("", h.User.List)
	users.GET("/:id", h.User.Get)
	users.PATCH("/:id", h.User.Update)
	users.DELETE("/:id", h.User.Delete)

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"code": 40400, "message": "route not found"})
	})
	return r
}
