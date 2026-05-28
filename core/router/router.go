package router

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Middleware is the same shape as middleware.Middleware: a function
// that wraps an http.Handler with additional behavior. Declared as a
// type alias so values produced by core/middleware (and batteries that
// return middleware.Middleware) can be passed to Router.Use without
// an explicit conversion.
type Middleware = middleware.Middleware

// Router wraps http.ServeMux with method-based routing, path parameter
// extraction, middleware chaining, and route grouping.
//
// It uses the Go 1.22+ ServeMux pattern syntax (e.g. "GET /users/{id}")
// which natively supports method matching and path parameter capture.
//
// The middleware chain is resolved per request and protected by an
// RWMutex. Concurrent Use() and ServeHTTP() are safe — useful when
// plugins / batteries / OnStart hooks contribute middleware while
// requests are already flowing.
type Router struct {
	mux      *http.ServeMux
	prefix   string
	notFound http.Handler
	parent   *Router

	mu          sync.RWMutex
	middlewares []Middleware
	patterns    []RegisteredRoute // populated by Handle for introspection

	// root is the topmost ancestor; chainVersion lives there. Any Use
	// anywhere in the tree bumps root.chainVersion atomically, which
	// invalidates every per-route cached handler in the tree.
	root         *Router
	chainVersion atomic.Uint64
}

// RegisteredRoute is the (method, pattern) pair returned by
// Router.Routes(). Used by framework introspection tooling so an
// agent / debug endpoint can enumerate what's mounted.
type RegisteredRoute struct {
	Method  string
	Pattern string
}

// New creates a new Router.
func New() *Router {
	r := &Router{mux: http.NewServeMux()}
	r.root = r
	return r
}

// Handle registers a handler for the given method and pattern.
// The pattern uses Go 1.22+ ServeMux syntax, e.g. "GET /users/{id}".
//
// The middleware chain is resolved per-request, not at registration time,
// so middleware appended via Use AFTER Handle still wraps this handler.
// This lets plugins contribute middleware from their Init without forcing
// a strict register-middleware-first ordering.
//
// The composed handler is cached per route; a route handles steady-state
// traffic with a single atomic load. Any Use anywhere in the router tree
// bumps the root chain-version and forces the next request on each route
// to recompose.
func (r *Router) Handle(method, pattern string, handler http.Handler) {
	fullPath := r.prefix + pattern
	fullPattern := method + " " + fullPath
	route := &cachedRoute{raw: handler, router: r}
	r.mux.Handle(fullPattern, route)
	// Record on the ROOT so a single Routes() call returns everything
	// registered under the tree, including via Groups.
	r.root.mu.Lock()
	r.root.patterns = append(r.root.patterns, RegisteredRoute{Method: method, Pattern: fullPath})
	r.root.mu.Unlock()
}

// Get registers a handler for GET requests on the given pattern.
func (r *Router) Get(pattern string, handler http.Handler) {
	r.Handle(http.MethodGet, pattern, handler)
}

// Post registers a handler for POST requests on the given pattern.
func (r *Router) Post(pattern string, handler http.Handler) {
	r.Handle(http.MethodPost, pattern, handler)
}

// Put registers a handler for PUT requests on the given pattern.
func (r *Router) Put(pattern string, handler http.Handler) {
	r.Handle(http.MethodPut, pattern, handler)
}

// Delete registers a handler for DELETE requests on the given pattern.
func (r *Router) Delete(pattern string, handler http.Handler) {
	r.Handle(http.MethodDelete, pattern, handler)
}

// Patch registers a handler for PATCH requests on the given pattern.
func (r *Router) Patch(pattern string, handler http.Handler) {
	r.Handle(http.MethodPatch, pattern, handler)
}

// Param extracts a single path parameter by name from the request.
// It uses the Go 1.22+ r.PathValue() method.
//
// SECURITY: the returned value is truncated at the first CR, LF, or
// NUL byte so a path-parameter payload can't be smuggled into
// downstream headers, log lines, SSE frames, or query strings.
func Param(r *http.Request, name string) string {
	return sanitizeParam(r.PathValue(name))
}

// Params extracts all path parameters from the request.
// It scans the registered pattern for {name} placeholders and extracts
// each value using r.PathValue().
//
// SECURITY: every value is truncated at the first CR / LF / NUL byte
// — see [Param].
func Params(r *http.Request) map[string]string {
	pattern := r.Pattern
	if pattern == "" {
		return nil
	}
	params := make(map[string]string)
	// Extract param names from the pattern, e.g. "/users/{id}/posts/{postId}"
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '{' {
			end := strings.IndexByte(pattern[i:], '}')
			if end == -1 {
				break
			}
			name := pattern[i+1 : i+end]
			val := sanitizeParam(r.PathValue(name))
			if val != "" {
				params[name] = val
			}
			i += end
		}
	}
	return params
}

// sanitizeParam truncates s at the first CR, LF, or NUL byte so a
// path-parameter value cannot be used to inject header lines, log
// lines, SSE frames, or any other line-delimited downstream protocol.
func sanitizeParam(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '\n' || c == '\r' || c == 0 {
			return s[:i]
		}
	}
	return s
}

// Routes returns the set of (method, pattern) pairs registered via
// Handle on this router and its child Groups. Order matches
// registration. Safe to call concurrently with Handle / Use.
//
// Used by framework introspection tooling to enumerate the mounted
// surface; not consulted on the request hot path.
//
// SECURITY: the returned slice includes EVERY registered pattern,
// including admin-only paths. Don't expose this output to anonymous
// callers as-is — wrap it in an auth gate, or use [RoutesFiltered]
// to drop patterns that match a deny predicate.
func (r *Router) Routes() []RegisteredRoute {
	r.root.mu.RLock()
	defer r.root.mu.RUnlock()
	out := make([]RegisteredRoute, len(r.root.patterns))
	copy(out, r.root.patterns)
	return out
}

// RoutesFiltered returns the set of registered routes EXCLUDING any
// pattern for which hide(route) returns true. Use this when exposing
// the route list over a public introspection endpoint so admin paths
// aren't trivially enumerated.
//
// Typical pattern:
//
//	r.RoutesFiltered(func(rt router.RegisteredRoute) bool {
//	    return strings.HasPrefix(rt.Pattern, "/admin/") ||
//	        strings.HasPrefix(rt.Pattern, "/internal/")
//	})
//
// hide may be nil — that case returns every route (equivalent to
// [Routes]).
func (r *Router) RoutesFiltered(hide func(RegisteredRoute) bool) []RegisteredRoute {
	all := r.Routes()
	if hide == nil {
		return all
	}
	out := make([]RegisteredRoute, 0, len(all))
	for _, rt := range all {
		if hide(rt) {
			continue
		}
		out = append(out, rt)
	}
	return out
}

// Use adds middleware to the router. Middleware is applied in the order
// they are added: the first middleware is the outermost wrapper.
//
// Safe to call concurrently with in-flight ServeHTTP — the mutation is
// guarded by an RWMutex. Bumps the root chain-version so every cached
// per-route handler in the tree recomposes on the next request.
func (r *Router) Use(mw ...Middleware) {
	if len(mw) == 0 {
		return
	}
	r.mu.Lock()
	r.middlewares = append(r.middlewares, mw...)
	r.mu.Unlock()
	r.root.chainVersion.Add(1)
}

// cachedRoute holds a registered route's raw handler plus an atomically
// cached version of it composed with the current middleware chain. The
// cache invalidates whenever the root router's chain-version moves.
type cachedRoute struct {
	raw       http.Handler
	router    *Router
	cached    atomic.Pointer[http.Handler]
	cachedV   atomic.Uint64
	composeMu sync.Mutex
}

func (c *cachedRoute) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	curVer := c.router.root.chainVersion.Load()
	if h := c.cached.Load(); h != nil && c.cachedV.Load() == curVer {
		(*h).ServeHTTP(w, req)
		return
	}
	c.composeMu.Lock()
	if h := c.cached.Load(); h != nil && c.cachedV.Load() == curVer {
		c.composeMu.Unlock()
		(*h).ServeHTTP(w, req)
		return
	}
	composed := c.router.wrap(c.raw)
	c.cached.Store(&composed)
	c.cachedV.Store(curVer)
	c.composeMu.Unlock()
	composed.ServeHTTP(w, req)
}

// Group creates a sub-router with the given path prefix and optional middleware.
// The sub-router inherits its parent's middleware chain — resolved at
// request time, so middleware added to the parent after Group still
// participates. NotFound is resolved up the parent chain at request
// time as well; a sub-router has no notFound of its own unless one is
// explicitly registered.
func (r *Router) Group(prefix string, mw ...Middleware) *Router {
	own := make([]Middleware, 0, len(mw))
	own = append(own, mw...)
	g := &Router{
		mux:         r.mux,
		prefix:      r.prefix + prefix,
		middlewares: own,
		parent:      r,
		root:        r.root,
	}
	if len(own) > 0 {
		r.root.chainVersion.Add(1)
	}
	return g
}

// effectiveNotFound returns the nearest non-nil NotFound handler in
// the parent chain (this router → parent → ...). Used by ServeHTTP so
// a sub-router served standalone falls back to the parent's NotFound
// even when set after Group.
func (r *Router) effectiveNotFound() http.Handler {
	if r.notFound != nil {
		return r.notFound
	}
	if r.parent != nil {
		return r.parent.effectiveNotFound()
	}
	return nil
}

// NotFound sets a custom handler for 404 (Not Found) responses.
// The router's middleware chain wraps the handler at request time, so 404
// responses go through the same recovery, logging, security headers, etc.
// as matched routes — and middleware added after NotFound still applies.
//
// Internally wrapped in a cachedRoute so the chain composition is
// memoised between Use bumps.
func (r *Router) NotFound(handler http.Handler) {
	r.notFound = &cachedRoute{raw: handler, router: r}
}

// NOTE: Go 1.22+ ServeMux handles 405 Method Not Allowed responses
// natively. There is no way to intercept or customise this behaviour
// through ServeMux, so a MethodNotAllowed API has been intentionally
// omitted. If you need custom 405 handling, wrap the Router with
// middleware that checks r.Pattern after ServeHTTP returns.

// ServeHTTP implements http.Handler. It dispatches requests through the
// underlying ServeMux. If no route matches and a custom notFound handler
// is set, it delegates to that handler (already a cachedRoute, so the
// chain composition is memoised between Use bumps).
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	_, pattern := r.mux.Handler(req)

	if pattern == "" {
		if nf := r.effectiveNotFound(); nf != nil {
			nf.ServeHTTP(w, req)
			return
		}
		r.mux.ServeHTTP(w, req)
		return
	}

	r.mux.ServeHTTP(w, req)
}

// effectiveChain returns the full middleware chain for this router,
// composed parent-first → own. Resolved per call so additions to any
// router in the chain take effect immediately.
//
// Holds a read lock while snapshotting r.middlewares, then recurses
// into the parent. The snapshot is a copy so the caller can iterate
// without holding the lock and a concurrent Use cannot mutate the
// slice the caller is walking.
func (r *Router) effectiveChain() []Middleware {
	r.mu.RLock()
	own := make([]Middleware, len(r.middlewares))
	copy(own, r.middlewares)
	r.mu.RUnlock()

	if r.parent == nil {
		return own
	}
	parent := r.parent.effectiveChain()
	if len(own) == 0 {
		return parent
	}
	out := make([]Middleware, 0, len(parent)+len(own))
	out = append(out, parent...)
	out = append(out, own...)
	return out
}

// wrap applies the router's middleware chain to the given handler.
// Middleware is applied in order: the first in the list wraps the outside,
// so it executes first on the way in and last on the way out.
func (r *Router) wrap(handler http.Handler) http.Handler {
	chain := r.effectiveChain()
	for i := len(chain) - 1; i >= 0; i-- {
		handler = chain[i](handler)
	}
	return handler
}
