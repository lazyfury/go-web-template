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

// Pinger is the subset of *sql.DB used by the readiness probe. Defining the
// interface here (consumer side) keeps this package free of database imports
// and makes the probe testable with a fake.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Health serves liveness and readiness probes.
type Health struct {
	db Pinger
}

// NewHealth creates a Health handler backed by the given database pinger.
func NewHealth(db Pinger) *Health {
	return &Health{db: db}
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
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
