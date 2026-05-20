package router

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// Middleware is a function that wraps an http.Handler with additional behavior.
type Middleware func(http.Handler) http.Handler

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

	// root is the topmost ancestor; chainVersion lives there. Any Use
	// anywhere in the tree bumps root.chainVersion atomically, which
	// invalidates every per-route cached handler in the tree.
	root         *Router
	chainVersion atomic.Uint64
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
	fullPattern := method + " " + r.prefix + pattern
	route := &cachedRoute{raw: handler, router: r}
	r.mux.Handle(fullPattern, route)
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
func Param(r *http.Request, name string) string {
	return r.PathValue(name)
}

// Params extracts all path parameters from the request.
// It scans the registered pattern for {name} placeholders and extracts
// each value using r.PathValue().
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
			val := r.PathValue(name)
			if val != "" {
				params[name] = val
			}
			i += end
		}
	}
	return params
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
