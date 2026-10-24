package routekit

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Group wraps a gin router group and records metadata for every route.
type Group struct {
	rg       *gin.RouterGroup
	reg      *Registry
	tags     []string
	security []string
}

type groupConfig struct {
	tags     []string
	security []string
}

// GroupOption customizes a child group.
type GroupOption func(*groupConfig)

// Tags adds tags to every operation registered on the group.
func Tags(names ...string) GroupOption {
	return func(cfg *groupConfig) {
		cfg.tags = append(cfg.tags, names...)
	}
}

// Security adds security scheme names to every operation on the group.
func Security(names ...string) GroupOption {
	return func(cfg *groupConfig) {
		cfg.security = append(cfg.security, names...)
	}
}

// NewGroup creates a metadata-aware wrapper around a gin router group.
func NewGroup(rg *gin.RouterGroup, reg *Registry) *Group {
	if rg == nil || reg == nil {
		panic("routekit: NewGroup requires a non-nil router group and registry")
	}
	return &Group{rg: rg, reg: reg}
}

// Group creates a child group that inherits tags and security.
func (g *Group) Group(relativePath string, opts ...GroupOption) *Group {
	cfg := groupConfig{
		tags:     append([]string(nil), g.tags...),
		security: append([]string(nil), g.security...),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Group{
		rg:       g.rg.Group(relativePath),
		reg:      g.reg,
		tags:     cfg.tags,
		security: cfg.security,
	}
}

// GET registers a GET route with metadata.
func (g *Group) GET(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodGet, relativePath, op, handler)
}

// POST registers a POST route with metadata.
func (g *Group) POST(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodPost, relativePath, op, handler)
}

// PUT registers a PUT route with metadata.
func (g *Group) PUT(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodPut, relativePath, op, handler)
}

// PATCH registers a PATCH route with metadata.
func (g *Group) PATCH(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodPatch, relativePath, op, handler)
}

// DELETE registers a DELETE route with metadata.
func (g *Group) DELETE(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodDelete, relativePath, op, handler)
}

// HEAD registers a HEAD route with metadata.
func (g *Group) HEAD(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodHead, relativePath, op, handler)
}

// OPTIONS registers an OPTIONS route with metadata.
func (g *Group) OPTIONS(relativePath string, op *Op, handler gin.HandlerFunc) {
	g.Handle(http.MethodOptions, relativePath, op, handler)
}

// Handle registers a route on the underlying gin group and records its
// metadata. Group tags and security are merged into the operation.
func (g *Group) Handle(method, relativePath string, op *Op, handler gin.HandlerFunc) {
	if op == nil {
		g.reg.errs = append(g.reg.errs, fmt.Errorf("routekit: %s %s: nil Op", method, relativePath))
		return
	}
	if handler == nil {
		g.reg.errs = append(g.reg.errs, fmt.Errorf("routekit: %s %s: nil handler", method, relativePath))
		return
	}

	metadata := *op
	metadata.Tags = dedupeStrings(append(append([]string(nil), g.tags...), op.Tags...))
	metadata.Security = dedupeStrings(append(append([]string(nil), g.security...), op.Security...))

	fullPath := joinPaths(g.rg.BasePath(), relativePath)
	specPath := stripBasePath(fullPath, g.reg.cfg.BasePath)
	if !g.reg.Add(method, fullPath, specPath, &metadata) {
		return
	}
	g.rg.Handle(method, relativePath, handler)
}

// Registry returns the registry backing this group.
func (g *Group) Registry() *Registry {
	return g.reg
}
