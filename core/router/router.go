package router

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Middleware is an alias of middleware.Middleware, so core/middleware values pass to Use unconverted.
type Middleware = middleware.Middleware

// Router routes requests by method and Go 1.22 ServeMux pattern through a per-request middleware chain, and is safe for concurrent Use and ServeHTTP.
type Router struct {
	mux    *http.ServeMux
	prefix string
	parent *Router

	mu          sync.RWMutex
	middlewares []Middleware
	patterns    []RegisteredRoute

	inner []Middleware

	timeout          *time.Duration
	routeTimeouts    map[string]time.Duration
	timeoutOverrides atomic.Bool

	notFound         http.Handler
	methodNotAllowed http.Handler

	fallbackGroups []*Router

	probeMu      sync.Mutex
	probeMux     *http.ServeMux
	probeMethods []string
	probeN       int

	root         *Router
	chainVersion atomic.Uint64

	registerHook func(method, pattern string)

	routeGate func(pattern string) bool

	serveHook func(method, pattern string)

	preRoute atomic.Pointer[func(*http.Request) (*http.Request, string)]

	resource string
}

// RegisteredRoute is one (method, pattern) pair returned by Routes.
type RegisteredRoute struct {
	Method  string
	Pattern string
}

// New returns an empty root Router.
func New() *Router {
	r := &Router{mux: http.NewServeMux()}
	r.root = r
	return r
}

// Handle registers handler for method and pattern (ServeMux syntax under r's prefix); middleware added later still wraps it.
// It panics on a pattern conflict or a malformed pattern.
func (r *Router) Handle(method, pattern string, handler http.Handler) {
	fullPath := r.prefix + pattern
	fullPattern := method + " " + fullPath
	route := newCachedRoute(handler, r, method, fullPath)
	route.gateKey = fullPattern
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				if !strings.Contains(fmt.Sprint(rec), "conflicts with") {
					panic(fmt.Sprintf("router: invalid route %q: %v", fullPattern, rec))
				}
				panic(fmt.Sprintf("router: route conflict registering %q: %v\n"+
					"Two registrations want the same path (commonly an auto-generated "+
					"entity/CRUD route and a page at the same name). Mount one elsewhere, "+
					"or move the generated routes under a path prefix (e.g. an API prefix).",
					fullPattern, rec))
			}
		}()
		r.mux.Handle(fullPattern, route)
	}()
	r.root.mu.Lock()
	r.root.patterns = append(r.root.patterns, RegisteredRoute{Method: method, Pattern: fullPath})
	hook := r.root.registerHook
	r.root.mu.Unlock()
	if hook != nil {
		hook(method, fullPath)
	}
}

// Get registers a GET handler.
func (r *Router) Get(pattern string, handler http.Handler) {
	r.Handle(http.MethodGet, pattern, handler)
}

// Post registers a POST handler.
func (r *Router) Post(pattern string, handler http.Handler) {
	r.Handle(http.MethodPost, pattern, handler)
}

// Put registers a PUT handler.
func (r *Router) Put(pattern string, handler http.Handler) {
	r.Handle(http.MethodPut, pattern, handler)
}

// Delete registers a DELETE handler.
func (r *Router) Delete(pattern string, handler http.Handler) {
	r.Handle(http.MethodDelete, pattern, handler)
}

// Patch registers a PATCH handler.
func (r *Router) Patch(pattern string, handler http.Handler) {
	r.Handle(http.MethodPatch, pattern, handler)
}

// Routes returns every route registered in the tree, in registration order, admin paths included.
func (r *Router) Routes() []RegisteredRoute {
	r.root.mu.RLock()
	defer r.root.mu.RUnlock()
	out := make([]RegisteredRoute, len(r.root.patterns))
	copy(out, r.root.patterns)
	return out
}

// RoutesFiltered returns Routes without those hide reports true for; a nil hide returns them all.
func (r *Router) RoutesFiltered(hide func(RegisteredRoute) bool) []RegisteredRoute {
	all := r.Routes()
	if hide == nil {
		return all
	}
	out := make([]RegisteredRoute, 0, len(all))
	for _, rt := range all {
		if !hide(rt) {
			out = append(out, rt)
		}
	}
	return out
}

type cachedRoute struct {
	raw       http.Handler
	router    *Router
	branch    *Router
	method    string
	pattern   string
	gateKey   string
	cached    atomic.Pointer[chainEntry]
	composeMu sync.Mutex
}

type chainEntry struct {
	h       http.Handler
	version uint64
}

func newCachedRoute(h http.Handler, r *Router, method, pattern string) *cachedRoute {
	return &cachedRoute{raw: h, router: r, method: method, pattern: pattern}
}

func (c *cachedRoute) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.router.root.mu.RLock()
	gate := c.router.root.routeGate
	served := c.router.root.serveHook
	c.router.root.mu.RUnlock()
	if gate != nil && c.gateKey != "" {
		if !routeGateSafely(req.Context(), gate, c.gateKey) {
			http.NotFound(w, req)
			return
		}
	}
	if served != nil {
		serveHookSafely(req.Context(), served, c.method, c.pattern)
	}
	if c.router.root.timeoutOverrides.Load() {
		if d, ok := c.router.effectiveRouteTimeout(c.method, c.pattern); ok {
			req = req.WithContext(middleware.WithRouteTimeout(req.Context(), middleware.RouteTimeout{
				Method:  c.method,
				Pattern: c.pattern,
				Budget:  d,
			}))
		}
	}
	if c.router.resource != "" || req.Context().Value(routeResourceKey{}) != nil {
		req = req.WithContext(context.WithValue(req.Context(), routeResourceKey{}, c.router.resource))
	}
	curVer := c.router.root.chainVersion.Load()
	if e := c.cached.Load(); e != nil && e.version == curVer {
		e.h.ServeHTTP(w, req)
		return
	}
	c.composeMu.Lock()
	if e := c.cached.Load(); e != nil && e.version == curVer {
		c.composeMu.Unlock()
		e.h.ServeHTTP(w, req)
		return
	}
	composed := c.router.wrapBranch(c.raw, c.branch)
	c.cached.Store(&chainEntry{h: composed, version: curVer})
	c.composeMu.Unlock()
	composed.ServeHTTP(w, req)
}

// Prefix returns r's full path prefix, every enclosing Group's prefix joined.
func (r *Router) Prefix() string { return r.prefix }

// Root returns the top of r's Group chain; routes registered there skip r's Group middleware.
func (r *Router) Root() *Router { return r.root }

// Group returns a sub-router under prefix with mw added to the inherited chain.
// A top-level prefix is empty, starts with "/", or is a portless host whose first segment has a dot or is localhost (any case); else it panics.
func (r *Router) Group(prefix string, mw ...Middleware) *Router {
	if r.prefix == "" {
		checkHostPrefix(prefix)
	}
	own := make([]Middleware, 0, len(mw))
	own = append(own, mw...)
	g := &Router{
		mux:         r.mux,
		prefix:      r.prefix + prefix,
		middlewares: own,
		parent:      r,
		root:        r.root,
		resource:    r.resource,
	}
	if len(own) > 0 {
		r.root.chainVersion.Add(1)
	}
	return g
}

func checkHostPrefix(prefix string) {
	if prefix == "" || prefix[0] == '/' {
		return
	}
	host, _, _ := strings.Cut(prefix, "/")
	switch {
	case strings.Contains(host, ":"):
		panic(fmt.Sprintf("router: Group(%q): a host prefix cannot carry a port; "+
			"ServeMux matches the request host without its port, so the routes would never match", prefix))
	case strings.Contains(host, ".") || strings.EqualFold(host, "localhost"):
		return
	}
	panic(fmt.Sprintf("router: Group(%q): a path prefix starts with \"/\" (use %q); "+
		"without it ServeMux reads %q as a host name and the routes never match a path", prefix, "/"+prefix, host))
}

// Use appends middleware to r's chain; the first added runs outermost.
func (r *Router) Use(mw ...Middleware) {
	if len(mw) == 0 {
		return
	}
	r.mu.Lock()
	r.middlewares = append(r.middlewares, mw...)
	r.mu.Unlock()
	r.root.chainVersion.Add(1)
}

// UseInner adds middleware to the whole tree, run inside every Use and Group middleware, next to the handler.
func (r *Router) UseInner(mw ...Middleware) {
	if len(mw) == 0 {
		return
	}
	root := r.root
	root.mu.Lock()
	root.inner = append(root.inner, mw...)
	root.mu.Unlock()
	root.chainVersion.Add(1)
}

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

func (r *Router) wrap(handler http.Handler) http.Handler {
	return r.wrapBranch(handler, nil)
}

func (r *Router) wrapBranch(handler http.Handler, branch *Router) http.Handler {
	r.root.mu.RLock()
	inner := slices.Clone(r.root.inner)
	r.root.mu.RUnlock()
	for _, c := range slices.Backward(inner) {
		handler = c(handler)
	}
	for cur := branch; cur != nil && !r.servedBy(cur); cur = cur.parent {
		cur.mu.RLock()
		own := slices.Clone(cur.middlewares)
		cur.mu.RUnlock()
		for _, c := range slices.Backward(own) {
			handler = c(handler)
		}
	}
	chain := r.effectiveChain()
	for _, c := range slices.Backward(chain) {
		handler = c(handler)
	}
	return handler
}
