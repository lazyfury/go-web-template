// Package web is the compact HTTP transport layer: engine assembly, the
// middleware chain, operational endpoints and the server lifecycle. It knows
// nothing about business domains; each domain registers its own routes.
package web

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SchemaStatus is the migration state of the database, as far as readiness is
// concerned. The transport layer defines it instead of importing a DB package:
// the composition root adapts whatever inspector it uses.
type SchemaStatus struct {
	Applied    int64
	Required   int64
	Dirty      bool
	Found      bool
	CoreTables bool
}

// SchemaProbe reports the schema status. It runs on every readiness request, so
// implementations must stay cheap (two catalog lookups plus one row read).
type SchemaProbe func(ctx context.Context) (SchemaStatus, error)

// HealthOption customizes Health.
type HealthOption func(*Health)

// Pinger is the subset of *sql.DB used by the readiness probe. Defining the
// interface here (consumer side) keeps this package free of database imports
// and makes the probe testable with a fake.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Health serves liveness and readiness probes.
type Health struct {
	db     Pinger
	schema SchemaProbe
}

// NewHealth creates a Health handler backed by the given database pinger.
func NewHealth(db Pinger, opts ...HealthOption) *Health {
	h := &Health{db: db}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithSchemaProbe makes readiness report migration drift (behind, dirty or
// never applied) instead of only "the database answers ping". A deployment that
// forgot `make migrate-up` otherwise keeps receiving traffic and fails every
// query with a 500.
func WithSchemaProbe(probe SchemaProbe) HealthOption {
	return func(h *Health) { h.schema = probe }
}

// Liveness reports whether the process is up. It never depends on external
// systems so orchestrators can distinguish a dead process from a degraded one.
func (h *Health) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readiness reports whether the service can serve traffic, including a
// database connectivity check.
func (h *Health) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "database unreachable"})
		return
	}
	if h.schema != nil {
		status, err := h.schema(ctx)
		switch {
		case err != nil:
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "schema status unavailable"})
			return
		case status.Dirty:
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "schema migration dirty (manual repair required)"})
			return
		case !status.Found && !status.CoreTables:
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "schema not applied (run migrations)"})
			return
		case status.Found && status.Applied < status.Required:
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":           "unavailable",
				"reason":           "schema behind",
				"applied_version":  status.Applied,
				"required_version": status.Required,
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
