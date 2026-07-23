package router

import (
	"fmt"
	"net/http"
	"sort"
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
	mux              *http.ServeMux
	prefix           string
	notFound         http.Handler
	methodNotAllowed http.Handler
	parent           *Router

	mu          sync.RWMutex
	middlewares []Middleware
	patterns    []RegisteredRoute // populated by Handle for introspection

	// root is the topmost ancestor; chainVersion lives there. Any Use
	// anywhere in the tree bumps root.chainVersion atomically, which
	// invalidates every per-route cached handler in the tree.
	root         *Router
	chainVersion atomic.Uint64

	// registerHook, when set on the ROOT router, is called for every
	// Handle call in the tree (subgroups funnel to root because pattern
	// recording at router.go:98 already targets r.root). Framework code
	// uses it to attribute routes to the module whose Init registered
	// them. Nil = no-op, zero overhead.
	registerHook func(method, pattern string)

	// routeGate, when set on the ROOT router, is checked in
	// cachedRoute.ServeHTTP BEFORE the middleware chain runs. Returning
	// false produces a plain 404 — used by the framework to gate routes
	// owned by a disabled module. The argument is the "METHOD /path"
	// key so two modules owning different methods on the same path are
	// gated independently. Read under r.mu. Nil = no gate. Stored on
	// root so a single Set call configures the whole tree.
	routeGate func(pattern string) bool
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
	route := &cachedRoute{raw: handler, router: r, method: method, pattern: fullPath}
	// net/http's ServeMux panics with a terse "conflicts with pattern" message
	// when two registrations want the same path. That's a common, confusing
	// failure — e.g. an auto-generated CRUD route and a page/screen both want
	// "/posts". Re-frame it with the colliding pattern and the usual fix so the
	// author isn't left decoding a mux internal. (Generic on purpose — this is
	// the framework-agnostic core layer.)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				panic(fmt.Sprintf("router: route conflict registering %q: %v\n"+
					"Two registrations want the same path (commonly an auto-generated "+
					"entity/CRUD route and a page at the same name). Mount one elsewhere, "+
					"or move the generated routes under a path prefix (e.g. an API prefix).",
					fullPattern, rec))
			}
		}()
		r.mux.Handle(fullPattern, route)
	}()
	// Record on the ROOT so a single Routes() call returns everything
	// registered under the tree, including via Groups.
	r.root.mu.Lock()
	r.root.patterns = append(r.root.patterns, RegisteredRoute{Method: method, Pattern: fullPath})
	hook := r.root.registerHook
	r.root.mu.Unlock()
	if hook != nil {
		hook(method, fullPath)
	}
}

// SetRegisterHook installs a callback fired for every Handle call across
// the router tree (subgroups funnel to root). Framework code uses it to
// attribute routes to the module whose Init registered them. Pass nil to
// clear. Must be called on the root router; setting on a child forwards
// to root.
func (r *Router) SetRegisterHook(fn func(method, pattern string)) {
	r.root.mu.Lock()
	r.root.registerHook = fn
	r.root.mu.Unlock()
}

// SetRouteGate installs a gate checked before the middleware chain for
// every matched route. The argument is the "METHOD /path" key (e.g.
// "GET /users/{id}") so two modules owning different methods on the
// same path are gated independently. Returning false produces a plain
// 404 (not 403 — a disabled module's existence must not leak). The gate
// is also consulted on the 405 path to exclude gated methods from the
// Allow header. Framework code uses it to gate routes owned by a
// disabled module. Pass nil to clear. Must be called on the root router;
// setting on a child forwards to root.
func (r *Router) SetRouteGate(fn func(pattern string) bool) {
	r.root.mu.Lock()
	r.root.routeGate = fn
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
			// Go's ServeMux stores a catch-all {name...} under the bare
			// "name" key, and {$} is the end-of-path anchor, not a param.
			// Normalise the extracted token so PathValue resolves and the
			// map exposes the param under its plain name — otherwise a
			// catch-all value is silently dropped and callers driving
			// auth/path logic off Params() fail open.
			name = strings.TrimSuffix(name, "...")
			if name == "$" {
				i += end
				continue
			}
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
		if !hide(rt) {
			out = append(out, rt)
		}
	}
	return out
}

// cachedRoute holds a registered route's raw handler plus an atomically
// cached version of it composed with the current middleware chain. The
type cachedRoute struct {
	raw    http.Handler
	router *Router
	method string // HTTP method (combined with pattern to form the gate key)
	// pattern is the full path pattern (prefix-joined, no method).
	// The gate key is method + " " + pattern so two modules can own
	// different methods on the same path independently.
	pattern   string
	cached    atomic.Pointer[http.Handler]
	cachedV   atomic.Uint64
	composeMu sync.Mutex
}

func (c *cachedRoute) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Route gate fires BEFORE the middleware chain so a disabled module's
	// path doesn't leak through auth, logging, or recovery. The gate key
	// is method + " " + pattern so two modules owning different methods
	// on the same path are gated independently. Read under RLock to avoid
	// a data race with SetRouteGate.
	c.router.root.mu.RLock()
	gate := c.router.root.routeGate
	c.router.root.mu.RUnlock()
	if gate != nil {
		key := c.method + " " + c.pattern
		if !gate(key) {
			http.NotFound(w, req)
			return
		}
	}
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

// effectiveMethodNotAllowed returns the nearest non-nil
// MethodNotAllowed handler in the parent chain (this router → parent
// → ...). Mirrors effectiveNotFound so a sub-router served standalone
// falls back to the parent's handler even when set after Group.
func (r *Router) effectiveMethodNotAllowed() http.Handler {
	if r.methodNotAllowed != nil {
		return r.methodNotAllowed
	}
	if r.parent != nil {
		return r.parent.effectiveMethodNotAllowed()
	}
	return nil
}

// MethodNotAllowed sets a custom handler for 405 (Method Not Allowed)
// responses. The router sets the RFC-compliant Allow header (filtered
// to exclude gated methods) BEFORE dispatching, so the handler inherits
// it without recomputing the allowed method set.
//
// The router's middleware chain wraps the handler at request time, so
// 405 responses go through the same recovery, logging, security
// headers, etc. as matched routes — and middleware added after
// MethodNotAllowed still applies. Mirrors [NotFound] exactly, including
// the cachedRoute memoisation.
func (r *Router) MethodNotAllowed(handler http.Handler) {
	r.methodNotAllowed = &cachedRoute{raw: handler, router: r}
}

// NOTE: Go 1.22+ ServeMux handles 405 Method Not Allowed responses
// natively. When a route gate is active, we intercept the 405 path to
// exclude gated-off methods from the Allow header so a disabled module's
// methods are not advertised.

// ServeHTTP implements http.Handler. It dispatches requests through the
// underlying ServeMux. If no route matches, it distinguishes a genuine
// 404 from a method mismatch (405): when the path exists under some
// non-gated method, it emits a 405 with the filtered Allow header;
// otherwise it falls through to the custom NotFound handler (if any)
// or the mux's native 404.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	_, pattern := r.mux.Handler(req)

	if pattern == "" {
		// ServeMux returns an empty pattern for BOTH a genuine 404 and a
		// method mismatch (405). allowedMethods resolves the path against
		// non-gated routes only: a non-empty set means the path exists
		// under some live method but the request's method isn't one of
		// them → 405; an empty set means genuine 404 (or all methods are
		// gated, which is indistinguishable from 404 by design).
		allowed := r.allowedMethods(req)
		if len(allowed) > 0 {
			// Set the RFC-compliant Allow header (filtered to exclude
			// gated methods) BEFORE dispatching, so the handler — custom
			// or default — inherits it without recomputing the set.
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			if mna := r.effectiveMethodNotAllowed(); mna != nil {
				mna.ServeHTTP(w, req)
				return
			}
			// The default 405 runs through the middleware chain just like
			// a custom handler (cachedRoute) would — CORS middleware must
			// see preflights whose path has no OPTIONS route. Cold path,
			// so the per-request wrap is fine.
			r.wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed),
					http.StatusMethodNotAllowed)
			})).ServeHTTP(w, req)
			return
		}
		// No non-gated methods match → 404. Use the custom NotFound
		// handler if set; otherwise a plain 404. We deliberately do NOT
		// fall through to the mux here: the mux would produce a 405 with
		// the full Allow header for a path that exists only under gated
		// methods, leaking the disabled module's registered methods.
		if nf := r.effectiveNotFound(); nf != nil {
			nf.ServeHTTP(w, req)
			return
		}
		// Default 404 also runs the chain, matching the custom-handler path.
		r.wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.NotFound(w, req)
		})).ServeHTTP(w, req)
		return
	}

	r.mux.ServeHTTP(w, req)
}

// allowedMethods returns the set of HTTP methods registered for the
// request's path, EXCLUDING methods whose route is gated-off. An empty
// result means the path either doesn't exist or all its methods are
// gated — in both cases a 404 is appropriate. A non-empty result means
// the path exists under some non-gated method but the request's own
// method isn't one of them — a 405 with the filtered Allow header.
//
// This runs only on the cold 404/405 fallback path, never on a matched
// route, so the per-call mux build is not on the request hot path.
func (r *Router) allowedMethods(req *http.Request) []string {
	r.root.mu.RLock()
	allPatterns := make([]RegisteredRoute, len(r.root.patterns))
	copy(allPatterns, r.root.patterns)
	gate := r.root.routeGate
	r.root.mu.RUnlock()
	if len(allPatterns) == 0 {
		return nil
	}

	// Build a method-agnostic probe mux from non-gated patterns only,
	// and track which methods each path pattern has.
	probeMux := http.NewServeMux()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	methodsByPath := make(map[string][]string)
	registered := make(map[string]bool)
	for _, rt := range allPatterns {
		if gate != nil {
			key := rt.Method + " " + rt.Pattern
			if !gate(key) {
				continue // gated route excluded from Allow set
			}
		}
		methodsByPath[rt.Pattern] = append(methodsByPath[rt.Pattern], rt.Method)
		if !registered[rt.Pattern] {
			registered[rt.Pattern] = true
			func() {
				defer func() { _ = recover() }()
				probeMux.Handle(rt.Pattern, noop)
			}()
		}
	}

	probe := req.Clone(req.Context())
	probe.Method = http.MethodGet
	_, matchedPattern := probeMux.Handler(probe)
	if matchedPattern == "" {
		return nil // path doesn't match any non-gated route
	}

	methods := methodsByPath[matchedPattern]
	sort.Strings(methods)
	return methods
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
