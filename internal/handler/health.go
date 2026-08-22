// Package handler contains the HTTP layer: request parsing, validation and
// response rendering. Handlers are thin; business rules live in services.
package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/response"
)

// HealthHandler serves liveness and readiness probes.
type HealthHandler struct {
	db *gorm.DB
}

// NewHealthHandler creates a HealthHandler.
func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Liveness reports whether the process is up. It never depends on external
// systems so orchestrators can distinguish a dead process from a degraded one.
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readiness reports whether the service can serve traffic, including a
// database connectivity check.
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if sqlDB, err := h.db.DB(); err != nil {
		response.Fail(c, err)
		return
	} else if err := sqlDB.PingContext(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "database unreachable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
