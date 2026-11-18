// Package routegroup groups routes under a shared prefix, middleware and access policy, as app.Group returns:
//
//	api.Use(authMiddleware)
package routegroup

import (
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

type GroupOption func(*RouteGroup)

// RouteGroup is routes under one prefix, middleware stack and access policy, with an OpenAPI tag and MCP namespace App.GroupEntity reads.
type RouteGroup struct {
	prefix  string
	sub     *router.Router
	subOnce sync.Once
	parent  *router.Router

	middleware []router.Middleware

	openapiTag string

	mcpNamespace string

	accessMW router.Middleware
}

// WithMiddleware adds group-scoped middleware to all routes in the group.
func WithMiddleware(mw ...router.Middleware) GroupOption {
	return func(g *RouteGroup) {
		g.middleware = append(g.middleware, mw...)
	}
}

// WithOpenAPITag tags the group's entity routes with tag instead of the entity name.
func WithOpenAPITag(tag string) GroupOption {
	return func(g *RouteGroup) {
		g.openapiTag = tag
	}
}

// WithMCPNamespace names the group's MCP tools "<ns>.<entity>.<action>".
func WithMCPNamespace(ns string) GroupOption {
	return func(g *RouteGroup) {
		g.mcpNamespace = ns
	}
}

// WithAccess sets an access gate that runs before every other group middleware.
func WithAccess(mw router.Middleware) GroupOption {
	return func(g *RouteGroup) {
		g.accessMW = mw
	}
}

// New creates a RouteGroup on parent under the normalised prefix.
func New(parent *router.Router, prefix string, opts ...GroupOption) *RouteGroup {
	prefix = normalizePrefix(prefix)
	g := &RouteGroup{
		prefix: prefix,
		parent: parent,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *RouteGroup) router_() *router.Router {
	g.subOnce.Do(func() {
		var mw []router.Middleware
		if g.accessMW != nil {
			mw = append(mw, g.accessMW)
		}
		mw = append(mw, g.middleware...)
		g.sub = g.parent.Group(g.prefix, mw...)
	})
	return g.sub
}

// Router returns the group's sub-router, for registrars that take a router.
func (g *RouteGroup) Router() *router.Router {
	return g.router_()
}

// Handle registers handler for method and pattern under the group's prefix.
func (g *RouteGroup) Handle(method, pattern string, handler http.Handler) {
	g.router_().Handle(method, pattern, handler)
}

// Get registers a GET handler within the group.
func (g *RouteGroup) Get(pattern string, handler http.Handler) {
	g.router_().Get(pattern, handler)
}

// Post registers a POST handler within the group.
func (g *RouteGroup) Post(pattern string, handler http.Handler) {
	g.router_().Post(pattern, handler)
}

// Put registers a PUT handler within the group.
func (g *RouteGroup) Put(pattern string, handler http.Handler) {
	g.router_().Put(pattern, handler)
}

// Delete registers a DELETE handler within the group.
func (g *RouteGroup) Delete(pattern string, handler http.Handler) {
	g.router_().Delete(pattern, handler)
}

// Patch registers a PATCH handler within the group.
func (g *RouteGroup) Patch(pattern string, handler http.Handler) {
	g.router_().Patch(pattern, handler)
}

// Use adds middleware to the group's sub-router.
func (g *RouteGroup) Use(mw ...router.Middleware) {
	g.router_().Use(mw...)
}

// Group creates a nested group that inherits this one's prefix and middleware.
func (g *RouteGroup) Group(prefix string, opts ...GroupOption) *RouteGroup {
	child := New(g.router_(), prefix, opts...)
	return child
}

// Prefix returns the group's full URL prefix, every enclosing group's included.
func (g *RouteGroup) Prefix() string {
	return g.router_().Prefix()
}

// OpenAPITag returns the configured OpenAPI tag, or empty string.
func (g *RouteGroup) OpenAPITag() string {
	return g.openapiTag
}

// MCPNamespace returns the configured MCP namespace, or empty string.
func (g *RouteGroup) MCPNamespace() string {
	return g.mcpNamespace
}

// MCPToolName returns "<namespace>.<entity>.<action>", or "<entity>.<action>" with no namespace.
func (g *RouteGroup) MCPToolName(entityName, action string) string {
	if g.mcpNamespace == "" {
		return entityName + "." + action
	}
	return g.mcpNamespace + "." + entityName + "." + action
}

func normalizePrefix(p string) string {
	if p == "" {
		return ""
	}
	p = textsafe.SanitizeControlBytes(p)
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "/" {
		return ""
	}
	return p
}
