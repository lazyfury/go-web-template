package user

import "github.com/gin-gonic/gin"

// RegisterRoutes mounts the user HTTP API on the given group. The group is
// expected to carry the /api/v1 middleware chain (rate limit, request
// timeout), so this module only owns its own paths.
func RegisterRoutes(rg *gin.RouterGroup, h *Handler) {
	users := rg.Group("/users")
	users.POST("", h.Create)
	users.GET("", h.List)
	users.GET("/:id", h.Get)
	users.PATCH("/:id", h.Update)
	users.DELETE("/:id", h.Delete)
}
