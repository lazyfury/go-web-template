package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/config"

	// Registers the generated OpenAPI spec (docs/swagger.json) with gin-swagger.
	_ "github.com/example/go-web-template/docs"
)

// New builds the gin engine with the global middleware chain, operational
// endpoints and the /api/v1 group. It returns the API group so the
// composition root can mount domain modules without this package importing
// them (web must not depend on business code).
func New(cfg *config.Config, logger *zap.Logger, health *Health) (*gin.Engine, *gin.RouterGroup) {
	if cfg.App.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// Global middleware. Recovery must run first so it can catch panics from
	// every other middleware; RequestID before Logger so logs carry the id.
	r.Use(
		Recovery(logger),
		RequestID(),
		Logger(logger),
		CORS(&cfg.CORS),
		Metrics(),
	)

	// Ops endpoints: liveness, readiness and Prometheus metrics. These are
	// intentionally not rate-limited so orchestrators can always probe.
	r.GET("/healthz", health.Liveness)
	r.GET("/readyz", health.Readiness)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if cfg.Swagger.Enabled {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	}

	// API v1. RateLimit stays outermost so abusive clients are rejected before
	// consuming a request deadline.
	v1 := r.Group("/api/v1",
		RateLimit(cfg.RateLimit.Enabled, cfg.RateLimit.RPS, cfg.RateLimit.Burst),
		Timeout(cfg.HTTPServer.RequestTimeout),
	)

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"code": 40400, "message": "route not found"})
	})
	return r, v1
}
