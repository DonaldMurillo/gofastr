package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
// RWMutex. Concurrent Use() and ServeHTTP() are safe, useful when
// plugins / batteries / OnStart hooks contribute middleware while
// requests are already flowing.
type Router struct {
	mux    *http.ServeMux
	prefix string
	parent *Router

	mu          sync.RWMutex
	middlewares []Middleware
	patterns    []RegisteredRoute // populated by Handle for introspection

	// timeout is this router's request-timeout override for every route
	// registered under it (SetTimeout). nil = inherit. Guarded by mu.
	// routeTimeouts (ROOT only) holds exact-route overrides keyed
	// "METHOD /full/pattern" (SetRouteTimeout). timeoutOverrides (ROOT
	// only) flips once any override exists so the per-request resolution
	// costs nothing until the feature is used.
	timeout          *time.Duration
	routeTimeouts    map[string]time.Duration
	timeoutOverrides atomic.Bool

	// notFound / methodNotAllowed are read on the request path by
	// effectiveNotFound / effectiveMethodNotAllowed and written by the
	// NotFound / MethodNotAllowed setters, which the package doc says
	// may run from OnStart while requests are already flowing. Both
	// sides go through mu.
	notFound         http.Handler
	methodNotAllowed http.Handler

	// probe* memoise the cold-path 404/405 mux. Guarded by probeMu, not
	// mu, so a rebuild never blocks route registration. See probe().
	probeMu      sync.Mutex
	probeMux     *http.ServeMux
	probeMethods map[string][]string
	probeN       int

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
	// false produces a plain 404, used by the framework to gate routes
	// owned by a disabled module. The argument is the "METHOD /path"
	// key so two modules owning different methods on the same path are
	// gated independently. Read under r.mu. Nil = no gate. Stored on
	// root so a single Set call configures the whole tree.
	routeGate func(pattern string) bool

	// serveHook, when set on the ROOT router, is called for every request
	// that matches a route, with the route's method and registered
	// pattern, not the request path, so "/users/42" reports as
	// "/users/{id}". Framework test tooling uses it to record which
	// routes a test run actually exercised (framework/semcov).
	//
	// It fires AFTER the route gate and BEFORE the middleware chain, so a
	// gated route is not recorded as reached and a route rejected later
	// by auth still is, reaching a route and being refused by it is
	// exactly the thing worth proving a test did.
	//
	// Read under r.mu on the request path. Nil = no-op, which is the
	// production case: nothing installs one outside tests.
	serveHook func(method, pattern string)
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
	// failure, e.g. an auto-generated CRUD route and a page/screen both want
	// "/posts". Re-frame it with the colliding pattern and the usual fix so the
	// author isn't left decoding a mux internal. (Generic on purpose: this is
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
// 404 (not 403: a disabled module's existence must not leak). The gate
// is also consulted on the 405 path to exclude gated methods from the
// Allow header. Framework code uses it to gate routes owned by a
// disabled module. Pass nil to clear. Must be called on the root router;
// setting on a child forwards to root.
func (r *Router) SetRouteGate(fn func(pattern string) bool) {
	r.root.mu.Lock()
	r.root.routeGate = fn
	r.root.mu.Unlock()
}

// SetServeHook installs a callback fired for every request that matches a
// route, with the route's method and its registered pattern (e.g. "GET",
// "/users/{id}") rather than the concrete request path. It runs after the
// route gate and before the middleware chain.
//
// Test tooling uses it to record semantic coverage: which routes a suite
// genuinely reached through the real router. Pass nil to clear. Must be
// called on the root router; setting on a child forwards to root.
func (r *Router) SetServeHook(fn func(method, pattern string)) {
	r.root.mu.Lock()
	r.root.serveHook = fn
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
// SECURITY: the returned value is truncated at the first byte that
// could not have arrived through normal path routing:
//
//   - CR, LF or NUL, so a payload can't be smuggled into downstream
//     headers, log lines, SSE frames, or query strings.
//   - "/" in a single-segment {name}: the mux matches one segment, so
//     a separator can only have arrived percent-encoded as %2F.
//   - a ".." path segment, in single-segment AND catch-all {name...}
//     form: the mux cleans dot segments out of the request path and
//     redirects, so a ".." that survives into a value likewise only
//     arrives %2E-encoded.
//
// A catch-all {name...} still spans segments: "a/b/c" is returned
// intact. That's the one form allowed to contain "/".
//
// This bounds, but does not replace, validation at the sink. A value
// that is safe as a header byte-string is not automatically safe as a
// filesystem path, an object-store key, or a map lookup; sinks still
// own their own allow-listing.
func Param(r *http.Request, name string) string {
	return sanitizePathParam(r.PathValue(name), isCatchAll(r.Pattern, name))
}

// isCatchAll reports whether pattern declares name as a {name...}
// multi-segment wildcard rather than a single-segment {name}.
func isCatchAll(pattern, name string) bool {
	return pattern != "" && strings.Contains(pattern, "{"+name+"...}")
}

// Params extracts all path parameters from the request.
// It scans the registered pattern for {name} placeholders and extracts
// each value using r.PathValue().
//
// SECURITY: every value is sanitized exactly as [Param] does.
//
// Every parameter the pattern declares is present in the map, even when
// its value sanitizes down to "". Omitting a scrubbed key made callers
// written as `if _, ok := p["id"]; !ok { ...treat as collection... }`
// fail OPEN: a scrubbed single-item request took the collection branch.
// Presence now means "declared", and the caller checks the value.
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
			// map exposes the param under its plain name. Otherwise a
			// catch-all value is silently dropped and callers driving
			// auth/path logic off Params() fail open.
			catchAll := strings.HasSuffix(name, "...")
			name = strings.TrimSuffix(name, "...")
			if name == "$" {
				i += end
				continue
			}
			params[name] = sanitizePathParam(r.PathValue(name), catchAll)
			i += end
		}
	}
	return params
}

// sanitizePathParam truncates s at the first byte a path parameter must
// never carry. See [Param] for the full contract.
//
// catchAll relaxes the "/" rule only: a {name...} wildcard is defined
// to span segments. The dot-segment rule applies to both forms: Go's
// mux resolves "." and ".." out of the request path (redirecting when
// it must), so a surviving ".." segment is always percent-encoded
// smuggling, never a legitimate route match.
func sanitizePathParam(s string, catchAll bool) string {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\n' || c == '\r' || c == 0:
			return s[:i]
		case c == '/' && !catchAll:
			return s[:i]
		case c == '.' && isDotDotSegment(s, i):
			return s[:i]
		}
	}
	return s
}

// isDotDotSegment reports whether a ".." path segment starts at i:
// that is, s[i:i+2] == ".." and it is bounded by "/" or the ends of the
// string on both sides. "..." and "a..b" are ordinary characters and
// are left alone.
func isDotDotSegment(s string, i int) bool {
	if i+2 > len(s) || s[i+1] != '.' {
		return false
	}
	if i > 0 && s[i-1] != '/' {
		return false
	}
	return i+2 == len(s) || s[i+2] == '/'
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
// callers as-is. Wrap it in an auth gate, or use [RoutesFiltered]
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
// hide may be nil. That case returns every route (equivalent to
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
	served := c.router.root.serveHook
	c.router.root.mu.RUnlock()
	if gate != nil {
		key := c.method + " " + c.pattern
		if !gate(key) {
			http.NotFound(w, req)
			return
		}
	}
	// Recorded after the gate, a gated route was not reached, and
	// before the chain, so a request the middleware later rejects still
	// counts as having exercised this route.
	if served != nil {
		served(c.method, c.pattern)
	}
	// Per-route timeout: stamp the resolution BEFORE the chain runs so
	// middleware.Timeout picks it up. Behind the root's atomic flag, an
	// app that never configures an override pays nothing here.
	if c.router.root.timeoutOverrides.Load() {
		if d, ok := c.router.effectiveRouteTimeout(c.method, c.pattern); ok {
			req = req.WithContext(middleware.WithRouteTimeout(req.Context(), middleware.RouteTimeout{
				Method:  c.method,
				Pattern: c.pattern,
				Budget:  d,
			}))
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

// Prefix returns the router's full path prefix: the composition of every
// enclosing Group's prefix. Registrars that need to address a sub-router's
// routes by URL (the entity MCP tools re-dispatch through the router) must
// use this, not the innermost segment.
func (r *Router) Prefix() string { return r.prefix }

// NoTimeout, passed to SetTimeout or SetRouteTimeout, exempts the
// route(s) from the request timeout entirely. A zero duration means the
// same thing (net/http convention: zero disables the deadline; see
// HTTPServerTimeoutsConfig). Prefer a finite budget; streaming handlers
// already shed the deadline on first Flush.
const NoTimeout time.Duration = -1

// SetTimeout sets the request-timeout budget for every route registered
// under this router (typically a Group). Routes resolve the nearest
// enclosing router with a timeout set; an exact SetRouteTimeout override
// wins over any group. Pass NoTimeout to exempt the group. The value is
// consumed by the default middleware's timeout
// (middleware.Timeout); with DisableRequestTimeout or without that
// middleware, it has no effect.
func (r *Router) SetTimeout(d time.Duration) {
	r.mu.Lock()
	r.timeout = &d
	r.mu.Unlock()
	r.root.timeoutOverrides.Store(true)
}

// SetRouteTimeout sets the request-timeout budget for one route. The
// pattern is relative to this router, exactly as passed to Handle; the
// override is keyed by the registered "METHOD /full/pattern" and must
// byte-match it — a key that matches no registered route (typo'd
// wildcard name, wrong method, wrong router) is silently inert and the
// route keeps the app-wide default. Pass NoTimeout (or zero) to exempt
// the route. See SetTimeout for how the value is consumed.
func (r *Router) SetRouteTimeout(method, pattern string, d time.Duration) {
	key := method + " " + r.prefix + pattern
	root := r.root
	root.mu.Lock()
	if root.routeTimeouts == nil {
		root.routeTimeouts = make(map[string]time.Duration)
	}
	root.routeTimeouts[key] = d
	root.mu.Unlock()
	root.timeoutOverrides.Store(true)
}

// effectiveRouteTimeout resolves the timeout override for a matched
// route: exact route override first, then the nearest enclosing router
// with SetTimeout, walking from the registering router to the root.
// ok=false means no override is configured and the app-wide default
// applies.
func (r *Router) effectiveRouteTimeout(method, pattern string) (time.Duration, bool) {
	root := r.root
	root.mu.RLock()
	d, ok := root.routeTimeouts[method+" "+pattern]
	root.mu.RUnlock()
	if ok {
		return d, true
	}
	for cur := r; cur != nil; cur = cur.parent {
		cur.mu.RLock()
		t := cur.timeout
		cur.mu.RUnlock()
		if t != nil {
			return *t, true
		}
	}
	return 0, false
}

// Group creates a sub-router with the given path prefix and optional middleware.
// The sub-router inherits its parent's middleware chain, resolved at
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
	r.mu.RLock()
	own := r.notFound
	r.mu.RUnlock()
	if own != nil {
		return own
	}
	if r.parent != nil {
		return r.parent.effectiveNotFound()
	}
	return nil
}

// NotFound sets a custom handler for 404 (Not Found) responses.
// The router's middleware chain wraps the handler at request time, so 404
// responses go through the same recovery, logging, security headers, etc.
// as matched routes, and middleware added after NotFound still applies.
//
// Internally wrapped in a cachedRoute so the chain composition is
// memoised between Use bumps.
func (r *Router) NotFound(handler http.Handler) {
	route := &cachedRoute{raw: handler, router: r}
	r.mu.Lock()
	replaced := r.notFound != nil
	r.notFound = route
	r.mu.Unlock()
	// The setter is last-write-wins, and the discarded handler may have
	// been load-bearing: a UI host dispatches every SCREEN through
	// NotFound, so an app installing a custom 404 after Mount silently
	// disables all pages (and the inverse order silently eats the app's
	// 404) — #258. Scream instead of guessing which side was intended.
	// (Generic on purpose: this is the framework-agnostic core layer;
	// the UI host's docs point at its own WithNotFoundScreen.)
	if replaced {
		slog.Warn("router: NotFound handler replaced; the previous handler is discarded",
			"hint", "compose with Router.WrapNotFound if both should run")
	}
}

// WrapNotFound wraps the router's NotFound fall-through with mw. mw receives
// the previously installed NotFound handler as next (a plain 404 when none
// was set) and returns the replacement. The wrapper sees exactly the
// requests the router would hand to NotFound (genuine misses; 405s and gated
// routes never reach it), and the middleware chain still wraps the result
// once at request time — delegating to next does NOT re-run the chain, so a
// wrapper adds behavior without doubling logging, request IDs, or headers.
//
// Call after the mounts that install a NotFound handler (e.g. after a UI
// host's Mount), so next is the handler being delegated to.
func (r *Router) WrapNotFound(mw func(next http.Handler) http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var next http.Handler = http.NotFoundHandler()
	// NotFound stores its handler wrapped in a cachedRoute; unwrap to the
	// raw one so delegation does not re-run the middleware chain.
	if cr, ok := r.notFound.(*cachedRoute); ok {
		next = cr.raw
	} else if r.notFound != nil {
		next = r.notFound
	}
	r.notFound = &cachedRoute{raw: mw(next), router: r}
}

// effectiveMethodNotAllowed returns the nearest non-nil
// MethodNotAllowed handler in the parent chain (this router → parent
// → ...). Mirrors effectiveNotFound so a sub-router served standalone
// falls back to the parent's handler even when set after Group.
func (r *Router) effectiveMethodNotAllowed() http.Handler {
	r.mu.RLock()
	own := r.methodNotAllowed
	r.mu.RUnlock()
	if own != nil {
		return own
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
// headers, etc. as matched routes, and middleware added after
// MethodNotAllowed still applies. Mirrors [NotFound] exactly, including
// the cachedRoute memoisation.
func (r *Router) MethodNotAllowed(handler http.Handler) {
	route := &cachedRoute{raw: handler, router: r}
	r.mu.Lock()
	r.methodNotAllowed = route
	r.mu.Unlock()
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
	h, pattern := r.mux.Handler(req)

	if pattern != "" {
		if _, matched := h.(*cachedRoute); matched {
			r.mux.ServeHTTP(w, req)
			return
		}
		// A non-empty pattern with a handler that ISN'T one of our routes
		// means net/http synthesised an internal redirect: trailing-slash
		// completion ("/a" → "/a/"), or path cleaning ("//a", "/a/./b",
		// "/a/../b"). Serving that straight out of the mux skips BOTH the
		// route gate and the entire middleware chain, which the 404 and
		// 405 branches below both honour. Two consequences, both real:
		//
		//   - The gate's documented contract is "returning false produces
		//     a plain 404: a disabled module's existence must not leak".
		//     A 307 here is exactly that leak, and Go's redirect preserves
		//     method and body, so it answers POSTs too.
		//   - No security headers, recovery, timeout, request-ID/logging,
		//     CORS or rate limiting run on the response.
		//
		// So: gate the redirect on its target route's key, and when it
		// survives, run it through the chain like any other response.
		if r.gateAllows(pattern) {
			r.wrap(h).ServeHTTP(w, req)
			return
		}
		// Gated target: fall through to the unmatched path so the reply
		// is byte-identical to a genuine 404.
	}

	{
		// ServeMux returns an empty pattern for BOTH a genuine 404 and a
		// method mismatch (405). allowedMethods resolves the path against
		// non-gated routes only: a non-empty set means the path exists
		// under some live method but the request's method isn't one of
		// them → 405; an empty set means genuine 404 (or all methods are
		// gated, which is indistinguishable from 404 by design).
		allowed := r.allowedMethods(req)
		if len(allowed) > 0 {
			// Set the RFC-compliant Allow header (filtered to exclude
			// gated methods) BEFORE dispatching, so the handler, custom
			// or default, inherits it without recomputing the set.
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			if mna := r.effectiveMethodNotAllowed(); mna != nil {
				mna.ServeHTTP(w, req)
				return
			}
			// The default 405 runs through the middleware chain just like
			// a custom handler (cachedRoute) would. CORS middleware must
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
}

// gateAllows reports whether the route gate permits the given
// "METHOD /path" key. No gate installed = allow.
func (r *Router) gateAllows(key string) bool {
	r.root.mu.RLock()
	gate := r.root.routeGate
	r.root.mu.RUnlock()
	return gate == nil || gate(key)
}

// allowedMethods returns the set of HTTP methods registered for the
// request's path, EXCLUDING methods whose route is gated-off. An empty
// result means the path either doesn't exist or all its methods are
// gated. In both cases a 404 is appropriate. A non-empty result means
// the path exists under some non-gated method but the request's own
// method isn't one of them: a 405 with the filtered Allow header.
//
// This runs only on the cold 404/405 fallback path, never on a matched
// route, so the per-call mux build is not on the request hot path.
func (r *Router) allowedMethods(req *http.Request) []string {
	probeMux, methodsByPath := r.probe()
	if probeMux == nil {
		return nil
	}

	probe := req.Clone(req.Context())
	probe.Method = http.MethodGet
	_, matchedPattern := probeMux.Handler(probe)
	if matchedPattern == "" {
		return nil // path doesn't match any registered route
	}

	// Gate-filter only the handful of methods on the matched pattern,
	// NOT every route in the table. An empty result means the path
	// exists but every method on it is gated off, which is a 404 by
	// design (advertising Allow for a method that answers 404 would be
	// a lie, and would leak the disabled module).
	var methods []string
	for _, m := range methodsByPath[matchedPattern] {
		if r.gateAllows(m + " " + matchedPattern) {
			methods = append(methods, m)
		}
	}
	sort.Strings(methods)
	return methods
}

// probe returns the memoised method-agnostic probe mux and its
// pattern→methods index, rebuilding only when new routes have been
// registered since the last build.
//
// This used to be rebuilt from scratch on EVERY unmatched request:
// cloning the request and re-parsing plus tree-inserting every
// registered pattern. Against 300 routes that measured ~205us and 3704
// allocations versus 170ns for a matched request: a ~1200x
// amplification driven entirely by an attacker-supplied URL, and it ran
// AHEAD of the rate limiter (which lives in the middleware chain the
// fallback only enters afterwards). r.root.patterns is append-only, so
// its length is a sufficient cache version.
func (r *Router) probe() (*http.ServeMux, map[string][]string) {
	root := r.root
	root.probeMu.Lock()
	defer root.probeMu.Unlock()

	root.mu.RLock()
	n := len(root.patterns)
	r.root.mu.RUnlock()
	if n == 0 {
		return nil, nil
	}
	if root.probeMux != nil && root.probeN == n {
		return root.probeMux, root.probeMethods
	}

	root.mu.RLock()
	allPatterns := make([]RegisteredRoute, n)
	copy(allPatterns, root.patterns[:n])
	root.mu.RUnlock()

	probeMux := http.NewServeMux()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	methodsByPath := make(map[string][]string)
	registered := make(map[string]bool)
	for _, rt := range allPatterns {
		methodsByPath[rt.Pattern] = append(methodsByPath[rt.Pattern], rt.Method)
		if !registered[rt.Pattern] {
			registered[rt.Pattern] = true
			func() {
				defer func() { _ = recover() }()
				probeMux.Handle(rt.Pattern, noop)
			}()
		}
	}

	root.probeMux = probeMux
	root.probeMethods = methodsByPath
	root.probeN = n
	return probeMux, methodsByPath
}

// Use adds middleware to the router. Middleware is applied in the order
// they are added: the first middleware is the outermost wrapper.
//
// Safe to call concurrently with in-flight ServeHTTP. The mutation is
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
	for _, c := range slices.Backward(chain) {
		handler = c(handler)
	}
	return handler
}
