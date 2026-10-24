package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/platform/config"
	"github.com/example/go-web-template/internal/platform/httpx"
	"github.com/example/go-web-template/internal/platform/routekit"
)

// clientIPKey is the default bucket dimension: the TCP peer IP, or the
// X-Forwarded-For entry when that peer is one of the configured trusted proxies
// (see SetTrustedProxies above). Callers cannot choose their own bucket.
func clientIPKey(c *gin.Context) string { return c.ClientIP() }

// New builds the gin engine with the global middleware chain, operational
// endpoints and the /api/v1 group. It returns a metadata-aware API group so
// the composition root can mount domain modules without this package
// importing them (web must not depend on business code). The group also
// records the route metadata used to build /openapi.json.
//
// limiters receives the /api/v1 guard and must be non-nil: the registry is owned
// by the composition root, which also hands it to cmd/server so idle buckets get
// swept by an owned goroutine instead of a constructor one.
func New(cfg *config.Config, logger *zap.Logger, health *Health, version string, limiters *Limiters) (*gin.Engine, *routekit.Group) {
	if limiters == nil {
		panic("web.New: limiters must not be nil (create it in the composition root and run it from cmd/server)")
	}
	if cfg.App.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	if version == "" {
		version = "dev"
	}

	r := gin.New()

	// Trust only the proxies configured for this deployment. gin's default
	// trusts every X-Forwarded-For, which lets any caller pick its own
	// rate-limit bucket and forge the IP written to the access log (black-box
	// drill: rotating X-Forwarded-For defeated the login limiter entirely).
	// An empty list means "trust nobody" and ClientIP falls back to RemoteAddr.
	if err := r.SetTrustedProxies(cfg.HTTPServer.TrustedProxies); err != nil {
		// Config validation already rejects malformed entries; never fall back
		// to trusting everyone if that invariant is ever broken.
		_ = r.SetTrustedProxies(nil)
		logger.Warn("invalid http.trusted_proxies; trusting no proxy", zap.Error(err))
	}

	// Global middleware. Recovery must run first so it can catch panics from
	// every other middleware; RequestID before Logger so logs carry the id.
	r.Use(
		Recovery(logger),
		RequestID(),
		Logger(logger),
		// Security headers run before CORS so even a rejected preflight carries
		// nosniff/XFO; HSTS only when the deployment terminates TLS (production).
		SecurityHeaders(cfg.IsProduction()),
		CORS(&cfg.CORS),
		Metrics(),
		BodyLimit(cfg.HTTPServer.MaxBodyBytes),
	)

	// Ops endpoints: liveness, readiness and (optionally) Prometheus metrics.
	// These are intentionally not rate-limited so orchestrators can always
	// probe. /metrics advertises the Go version, the route inventory and the
	// traffic volume, so a public-facing deployment turns it off and scrapes
	// through a sidecar / ACL'd proxy instead.
	r.GET("/healthz", health.Liveness)
	r.GET("/readyz", health.Readiness)
	if cfg.Metrics.Enabled {
		r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	}

	// API v1. RateLimit stays outermost so abusive clients are rejected before
	// consuming a request deadline.
	ginV1 := r.Group("/api/v1",
		// API payloads carry access tokens and per-user data: never cache them.
		NoStore(),
		limiters.Middleware("api", cfg.RateLimit.Enabled, clientIPKey, cfg.RateLimit.RPS, cfg.RateLimit.Burst),
		Timeout(cfg.HTTPServer.RequestTimeout),
	)

	registry := routekit.NewRegistry(routekit.Config{
		Info: routekit.Info{
			Title:       cfg.App.Name,
			Description: "HTTP JSON API for " + cfg.App.Name,
			Version:     version,
		},
		BasePath: "/api/v1",
		Servers:  []routekit.Server{{URL: "/api/v1", Description: "Current server"}},
		// Domains attach BearerAuth to protected operations; the scheme itself
		// is declared once here so the contract and the token format agree.
		SecuritySchemes: map[string]routekit.SecurityScheme{
			routekit.BearerAuth: routekit.BearerScheme(),
		},
	})
	v1 := routekit.NewGroup(ginV1, registry)

	r.NoRoute(func(c *gin.Context) {
		httpx.Error(c, http.StatusNotFound, apperr.CodeNotFound, "route not found")
	})
	return r, v1
}
