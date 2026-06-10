// Package routegroup provides the App-level route group abstraction.
//
// A RouteGroup clusters routes under a shared prefix, middleware stack,
// and optional access policy. Groups nest and compose with the existing
// router + middleware pipeline.
//
// Basic usage:
//
//	api := app.Group("/api")
//	api.Use(authMiddleware)
//	api.Entity("orders", entityConfig)  // mounts at /api/orders
//
// Nested groups:
//
//	v1 := app.Group("/api/v1")
//	v1.Entity("users", ...)
//	v2 := app.Group("/api/v2")
//	v2.Entity("users", ...)
//
// Access policy:
//
//	admin := app.Group("/admin", routegroup.WithAccess(access.RequirePermission("admin:access")))
//	admin.Entity("settings", ...)
package routegroup

import (
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// GroupOption configures a RouteGroup.
type GroupOption func(*RouteGroup)

// RouteGroup clusters routes under a shared prefix, middleware stack,
// and optional access policy. It delegates to the underlying
// core/router.Group for prefix and middleware chaining while adding
// App-level concerns: OpenAPI tagging and MCP tool namespacing.
//
// RouteGroup does NOT own entity registration — that stays on App.
// The group provides the router surface (Handle/Get/Post/...) and the
// metadata (prefix, OpenAPI tag, MCP namespace) that App.GroupEntity
// reads when mounting an entity into a group.
type RouteGroup struct {
	prefix  string
	sub     *router.Router // lazy-created router.Group
	subOnce sync.Once      // guards lazy init of sub
	parent  *router.Router

	// Middleware specific to this group (applied in addition to
	// parent middleware).
	middleware []router.Middleware

	// OpenAPI tag applied to all routes in this group. When empty,
	// the default per-entity tag is used.
	openapiTag string

	// MCP namespace prefix. When set, MCP tools registered for
	// entities in this group are prefixed as "<namespace>.<tool>".
	mcpNamespace string

	// Access policy middleware — typically RequirePermission or similar.
	// Applied as the outermost group-scoped middleware so it gates
	// everything inside.
	accessMW router.Middleware
}

// WithMiddleware adds group-scoped middleware to all routes in the group.
func WithMiddleware(mw ...router.Middleware) GroupOption {
	return func(g *RouteGroup) {
		g.middleware = append(g.middleware, mw...)
	}
}

// WithOpenAPITag sets the OpenAPI tag for all routes in this group.
// Entity routes that would normally be tagged by entity name instead
// receive this group-level tag, keeping the spec organized.
func WithOpenAPITag(tag string) GroupOption {
	return func(g *RouteGroup) {
		g.openapiTag = tag
	}
}

// WithMCPNamespace prefixes MCP tool names registered inside this group.
// E.g. WithMCPNamespace("admin") makes an entity "users" register
// MCP tools as "admin.users.list", "admin.users.get", etc.
func WithMCPNamespace(ns string) GroupOption {
	return func(g *RouteGroup) {
		g.mcpNamespace = ns
	}
}

// WithAccess applies an access-control middleware as the outermost
// group-scoped layer. Use it with access.RequirePermission or any
// similar gate. The middleware runs before any other group middleware
// and before the route handler.
func WithAccess(mw router.Middleware) GroupOption {
	return func(g *RouteGroup) {
		g.accessMW = mw
	}
}

// New creates a new RouteGroup on the given router with the given prefix.
// The group inherits parent middleware; any GroupOption adds to it.
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

// router returns the sub-router for this group, creating it lazily.
// Race-free: concurrent Get/Post/Use calls on a fresh group used to spawn
// multiple sub-routers and silently lose registrations.
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

// Router returns the underlying sub-router. Use this when you need to
// pass the group's router to external registrars (CRUD, Mountable, etc.).
func (g *RouteGroup) Router() *router.Router {
	return g.router_()
}

// Handle registers a handler for the given method and pattern within
// the group's prefix.
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

// Group creates a nested sub-group. The child inherits this group's
// prefix and middleware; its own options add to them.
func (g *RouteGroup) Group(prefix string, opts ...GroupOption) *RouteGroup {
	child := New(g.router_(), prefix, opts...)
	return child
}

// Prefix returns the full URL prefix for this group.
func (g *RouteGroup) Prefix() string {
	return g.prefix
}

// OpenAPITag returns the configured OpenAPI tag, or empty string.
func (g *RouteGroup) OpenAPITag() string {
	return g.openapiTag
}

// MCPNamespace returns the configured MCP namespace, or empty string.
func (g *RouteGroup) MCPNamespace() string {
	return g.mcpNamespace
}

// MCPToolName builds the full MCP tool name for an entity in this group.
// If a namespace is set, returns "<namespace>.<entity>.<action>".
// Otherwise returns the default "<entity>.<action>".
func (g *RouteGroup) MCPToolName(entityName, action string) string {
	if g.mcpNamespace == "" {
		return entityName + "." + action
	}
	return g.mcpNamespace + "." + entityName + "." + action
}

// normalizePrefix returns the canonical leading-slash, no-trailing-slash
// form of p. Empty / root prefixes collapse to "" (no prefix needed).
//
// Hardening applied in order: control bytes (CR/LF/NUL/C0) are stripped
// so a forged prefix can't smuggle a header line; backslashes are
// rewritten to forward slashes so a Windows-style path can't bypass a
// /admin gate; and path.Clean collapses repeated slashes and resolves
// `.` / `..` segments. The result is the prefix attached to every
// child route, so a non-canonical form here permanently aliases routes.
func normalizePrefix(p string) string {
	if p == "" {
		return ""
	}
	p = stripPrefixCtrlBytes(p)
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

func stripPrefixCtrlBytes(s string) string {
	hasCtrl := false
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			hasCtrl = true
			break
		}
	}
	if !hasCtrl {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
