package router

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
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

	// inner (ROOT only) is the UseInner list: middleware composed inside
	// every Use and Group middleware in the tree, next to the handler.
	// Guarded by mu.
	inner []Middleware

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
	// fallbackFor and written by the
	// NotFound / MethodNotAllowed setters, which the package doc says
	// may run from OnStart while requests are already flowing. Both
	// sides go through mu.
	notFound         http.Handler
	methodNotAllowed http.Handler

	// fallbackGroups (ROOT only) lists the Groups that set their own
	// NotFound or MethodNotAllowed, so a miss under a group's prefix runs
	// the group's handler even when the root serves the request. Guarded
	// by root.mu.
	fallbackGroups []*Router

	// probe* memoise the cold-path 404/405 mux. Guarded by probeMu, not
	// mu, so a rebuild never blocks route registration. See probe().
	probeMu      sync.Mutex
	probeMux     *http.ServeMux
	probeMethods []string
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

	// preRoute, when set on the ROOT router, runs on every request before
	// the mux matches it (PreRoute). An atomic pointer, so the request
	// path pays one load and no lock.
	preRoute atomic.Pointer[func(*http.Request) (*http.Request, string)]

	// resource names the REST resource every route registered on this
	// router serves (ResourceGroup); "" for none. Set before any route is
	// registered and never written again, so the request path reads it
	// without a lock.
	resource string
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
	route := newCachedRoute(handler, r, method, fullPath)
	// net/http's ServeMux panics with a terse "conflicts with pattern" message
	// when two registrations want the same path. That's a common, confusing
	// failure, e.g. an auto-generated CRUD route and a page/screen both want
	// "/posts". Re-frame it with the colliding pattern and the usual fix so the
	// author isn't left decoding a mux internal. (Generic on purpose: this is
	// the framework-agnostic core layer.)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				// Only a conflict gets the conflict advice: an unclean or
				// malformed pattern ("/api//x" from a Group prefix ending
				// in "/") is its own mistake, and naming a second
				// registration that does not exist sent authors hunting.
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
//   - an empty segment in a catch-all {name...}: a leading "/" or a
//     "//" can only arrive as %2F, for the same reason. A trailing "/"
//     is a real path's and stays.
//
// A catch-all {name...} still spans segments: "a/b/c" is returned
// intact. That's the one form allowed to contain "/".
//
// This bounds, but does not replace, validation at the sink. A value
// that is safe as a header byte-string is not automatically safe as a
// filesystem path, an object-store key, or a map lookup; sinks still
// own their own allow-listing.
//
// Param panics when the matched route declares no {name} wildcard: a
// misspelt name is a programming error, and returning "" let a handler
// carry on with an empty id. A request with no pattern (built by hand,
// path values set with SetPathValue) is not checked.
func Param(r *http.Request, name string) string {
	if r.Pattern != "" && !declaresWildcard(r.Pattern, name) {
		panic(fmt.Sprintf("router.Param: route %q has no {%s} wildcard", r.Pattern, name))
	}
	return SanitizePathParam(r.PathValue(name), isCatchAll(r.Pattern, name))
}

// declaresWildcard reports whether pattern has a {name} or {name...}
// wildcard.
func declaresWildcard(pattern, name string) bool {
	_, declared := wildcards(pattern)[name]
	return declared
}

// isCatchAll reports whether pattern declares name as a {name...}
// multi-segment wildcard rather than a single-segment {name}.
func isCatchAll(pattern, name string) bool {
	return wildcards(pattern)[name]
}

// wildcards maps each wildcard name a pattern declares to whether it is
// a {name...} catch-all. Go's ServeMux stores a catch-all under the
// bare name, and {$} is the end-of-path anchor, not a param, so
// "rest..." and "$" are never names.
func wildcards(pattern string) map[string]bool {
	out := map[string]bool{}
	for {
		start := strings.IndexByte(pattern, '{')
		if start == -1 {
			return out
		}
		end := strings.IndexByte(pattern[start:], '}')
		if end == -1 {
			return out
		}
		name := pattern[start+1 : start+end]
		pattern = pattern[start+end+1:]
		catchAll := strings.HasSuffix(name, "...")
		if name = strings.TrimSuffix(name, "..."); name != "$" {
			out[name] = catchAll
		}
	}
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
	// A catch-all is keyed by its bare name, so PathValue resolves it;
	// keyed as "rest..." its value was silently dropped and callers
	// driving auth/path logic off Params() failed open.
	params := make(map[string]string)
	for name, catchAll := range wildcards(pattern) {
		params[name] = SanitizePathParam(r.PathValue(name), catchAll)
	}
	return params
}

// SanitizePathParam truncates s at the first byte a path parameter must
// never carry. See [Param] for the full contract.
//
// catchAll relaxes the "/" rule only: a {name...} wildcard is defined
// to span segments, but not to hold an empty one except at the end.
// The dot-segment rule applies to both forms: Go's mux resolves "."
// and ".." out of the request path (redirecting when it must), so a
// surviving ".." segment is always percent-encoded
// smuggling, never a legitimate route match.
//
// Exported for readers that hold a name and a catch-all flag without a
// *http.Request. Prefer [Param], which derives the flag from the route
// pattern; core/handler's Bind uses it for exactly that reason.
func SanitizePathParam(s string, catchAll bool) string {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\n' || c == '\r' || c == 0:
			return s[:i]
		case c == '/' && !catchAll:
			return s[:i]
		case c == '/' && (i == 0 || s[i-1] == '/'):
			// An empty segment, leading or interior, in a catch-all: the
			// mux cleans "//" out of a real path, so it arrived as %2F
			// and would hand a sink an absolute path ("/etc/passwd").
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
	pattern string
	// gateKey is method + " " + pattern, so two modules can own
	// different methods on the same path independently. Built once at
	// registration; the gate check runs on every request.
	gateKey   string
	cached    atomic.Pointer[http.Handler]
	cachedV   atomic.Uint64
	composeMu sync.Mutex
}

func newCachedRoute(h http.Handler, r *Router, method, pattern string) *cachedRoute {
	return &cachedRoute{raw: h, router: r, method: method, pattern: pattern, gateKey: method + " " + pattern}
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
		if !gate(c.gateKey) {
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
	// The route's resource, stamped before the chain so a middleware
	// that authorizes by resource reads it from the route, not the path.
	// An unnamed route clears one it inherited: a handler that
	// redispatches its request through the router (the entity MCP tools)
	// must not hand the next route the first route's name.
	if c.router.resource != "" || req.Context().Value(routeResourceKey{}) != nil {
		req = req.WithContext(context.WithValue(req.Context(), routeResourceKey{}, c.router.resource))
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
//
// A top-level prefix starts with "/", or names a host
// ("api.example.com", "localhost"); anything else panics, since the mux
// would read "v1" in Group("v1") as a host and no path would match.
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

// checkHostPrefix refuses a top-level Group prefix that does not start
// with "/" unless its first segment reads as a host. ServeMux takes
// everything before the first "/" of a pattern as the host, so
// Group("v1").Get("/x") registered a route for Host "v1" and /v1/x
// answered 404, silently. A host group names a host: one with a dot
// ("api.example.com") or "localhost". A port is refused too: the mux
// matches the request's host with its port stripped, so a pattern
// host carrying one ("localhost:8080") can never match.
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

// fallbackFor picks the handler a miss runs: the one set on the most
// specific group that covers the request (moreSpecific: a group the
// root serves for), else the nearest one up r's own parent chain. pick
// reads the handler a router set for itself (nil for none). below, when
// set, limits the groups to those less specific than it: a group's
// WrapNotFound delegates there, so a chain of them always ends.
func (r *Router) fallbackFor(req *http.Request, pick func(*Router) http.Handler, below *Router) http.Handler {
	root := r.root
	root.mu.RLock()
	groups := slices.Clone(root.fallbackGroups)
	root.mu.RUnlock()
	belowAt := slices.Index(groups, below)
	if below != nil && belowAt < 0 {
		groups = nil // not registered yet: only the parent chain is safe
	}
	bestAt := -1
	var bestH http.Handler
	for i, g := range groups {
		if below != nil && (g == below || !moreSpecific(groups, belowAt, i)) {
			continue
		}
		if bestAt >= 0 && !moreSpecific(groups, i, bestAt) {
			continue
		}
		if !g.servedBy(r) || !prefixCovers(g.prefix, req) {
			continue
		}
		if h := pick(g); h != nil {
			bestAt, bestH = i, h
		}
	}
	if bestAt >= 0 {
		return bestH
	}
	for cur := r; cur != nil; cur = cur.parent {
		if cur == below {
			continue
		}
		if h := pick(cur); h != nil {
			return h
		}
	}
	return nil
}

// moreSpecific reports whether groups[a] outranks groups[b] for a miss
// both cover, the way the mux ranks patterns: a host group first, then
// the longer path, then, left to right, a literal segment over a
// wildcard. Registration order breaks a tie, so the order is total.
func moreSpecific(groups []*Router, a, b int) bool {
	if b < 0 {
		return true
	}
	ha, pa := splitGroupPrefix(groups[a].prefix)
	hb, pb := splitGroupPrefix(groups[b].prefix)
	if (ha != "") != (hb != "") {
		return ha != ""
	}
	if len(pa) != len(pb) {
		return len(pa) > len(pb)
	}
	for i := range pa {
		wa, wb := strings.HasPrefix(pa[i], "{"), strings.HasPrefix(pb[i], "{")
		if wa != wb {
			return !wa
		}
	}
	return a < b
}

// splitGroupPrefix splits a Group prefix into its host ("" for none)
// and its non-empty path segments.
func splitGroupPrefix(prefix string) (string, []string) {
	host := ""
	if prefix != "" && prefix[0] != '/' {
		host, prefix, _ = strings.Cut(prefix, "/")
	}
	var segs []string
	for seg := range strings.SplitSeq(prefix, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	return host, segs
}

// servedBy reports whether g is r or a Group under it, so serving a
// Group standalone never hands a miss to a sibling's handler.
func (g *Router) servedBy(r *Router) bool {
	for cur := g; cur != nil; cur = cur.parent {
		if cur == r {
			return true
		}
	}
	return false
}

// prefixCovers reports whether a Group prefix matches the start of the
// request, segment by segment, the way the mux would: a {name} matches
// any one segment and a {name...} the rest. A prefix that names a host
// ("api.example.com/v1") matches the request host without its port.
func prefixCovers(prefix string, req *http.Request) bool {
	if prefix == "" {
		return true
	}
	if prefix[0] != '/' {
		host, rest, _ := strings.Cut(prefix, "/")
		reqHost := req.Host
		if h, _, err := net.SplitHostPort(reqHost); err == nil {
			reqHost = h
		}
		if !strings.EqualFold(host, reqHost) {
			return false
		}
		prefix = "/" + rest
	}
	want := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
	got := strings.Split(req.URL.Path, "/")
	if len(got) < len(want) {
		return false
	}
	for i, seg := range want {
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "...}"):
			return true
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			if got[i] == "" {
				return false
			}
		case seg != got[i]:
			return false
		}
	}
	return true
}

// ownNotFound and ownMethodNotAllowed read the handler r set for
// itself, for fallbackFor.
func ownNotFound(r *Router) http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.notFound
}

func ownMethodNotAllowed(r *Router) http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.methodNotAllowed
}

// registerFallback records a Group that set its own NotFound or
// MethodNotAllowed on the root, once.
func (r *Router) registerFallback() {
	if r.parent == nil {
		return
	}
	root := r.root
	root.mu.Lock()
	if !slices.Contains(root.fallbackGroups, r) {
		root.fallbackGroups = append(root.fallbackGroups, r)
	}
	root.mu.Unlock()
}

// NotFound sets a custom handler for 404 (Not Found) responses.
// The router's middleware chain wraps the handler at request time, so 404
// responses go through the same recovery, logging, security headers, etc.
// as matched routes, and middleware added after NotFound still applies.
//
// On a Group it answers the misses under the group's prefix, even when
// the root router serves the request: a miss runs the NotFound of the
// longest-prefix group that set one, else the root's. MethodNotAllowed
// resolves the same way.
//
// Internally wrapped in a cachedRoute so the chain composition is
// memoised between Use bumps.
func (r *Router) NotFound(handler http.Handler) {
	route := newCachedRoute(handler, r, "", "")
	r.mu.Lock()
	replaced := r.notFound != nil
	r.notFound = route
	r.mu.Unlock()
	r.registerFallback()
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
// host's Mount), so next is the handler being delegated to. On a Group
// with no NotFound of its own, next is resolved per request instead: the
// next less specific group covering the miss, else the root's handler,
// whenever it was set.
func (r *Router) WrapNotFound(mw func(next http.Handler) http.Handler) {
	r.mu.Lock()
	prev := r.notFound
	var next http.Handler = http.NotFoundHandler()
	switch {
	case prev != nil:
		// NotFound stores its handler wrapped in a cachedRoute; unwrap to
		// the raw one so delegation does not re-run the middleware chain.
		if cr, ok := prev.(*cachedRoute); ok {
			next = cr.raw
		} else {
			next = prev
		}
	case r.parent != nil:
		// A Group with no NotFound of its own delegates to the handler a
		// miss here would otherwise get, resolved per request: the next
		// less specific group that covers it (a sibling Group("/api")
		// for Group("/api/v2")), else the root's, including one the root
		// sets after this call (a UI host Mount).
		g := r
		next = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			h := g.root.fallbackFor(req, ownNotFound, g)
			if cr, ok := h.(*cachedRoute); ok {
				h = cr.raw
			}
			if h == nil {
				h = http.NotFoundHandler()
			}
			h.ServeHTTP(w, req)
		})
	}
	r.notFound = newCachedRoute(mw(next), r, "", "")
	r.mu.Unlock()
	r.registerFallback()
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
	route := newCachedRoute(handler, r, "", "")
	r.mu.Lock()
	r.methodNotAllowed = route
	r.mu.Unlock()
	r.registerFallback()
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
	if fn := r.root.preRoute.Load(); fn != nil {
		next, target := (*fn)(req)
		if target != "" {
			r.wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, target, http.StatusPermanentRedirect)
			})).ServeHTTP(w, req)
			return
		}
		if next != nil {
			req = next
		}
	}
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
		//
		// For CONNECT, net/http returns the bare target path, not the
		// route's pattern. The route it completes to is methodless
		// (" /tree/") or a CONNECT route ("CONNECT /tree/"); gate both.
		allowed := r.gateAllows(pattern)
		if !strings.ContainsAny(pattern, " \t") {
			allowed = r.gateAllows(" "+pattern) && r.gateAllows(req.Method+" "+pattern)
		}
		if allowed {
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
			if mna := r.fallbackFor(req, ownMethodNotAllowed, nil); mna != nil {
				mna.ServeHTTP(w, req)
				return
			}
			// The default 405 runs through the middleware chain just like
			// a custom handler (cachedRoute) would. CORS middleware must
			// see preflights whose path has no OPTIONS route. Cold path,
			// so the per-request wrap is fine.
			r.wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				i18n.HTTPError(w, req, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			})).ServeHTTP(w, req)
			return
		}
		// No non-gated methods match → 404. Use the custom NotFound
		// handler if set; otherwise a plain 404. We deliberately do NOT
		// fall through to the mux here: the mux would produce a 405 with
		// the full Allow header for a path that exists only under gated
		// methods, leaking the disabled module's registered methods.
		if nf := r.fallbackFor(req, ownNotFound, nil); nf != nil {
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

// PreRoute installs fn to run on every request the tree serves, before
// the mux matches it. fn returns the request to route, usually with a
// rewritten URL and context, or nil to route the original. A non-empty
// target answers 308 Permanent Redirect to it, through the middleware
// chain, and routes nothing. The framework uses it to read a locale
// prefix off the path (framework.WithLocalePrefixes): route matching
// sees "/pricing" for "/es/pricing", and a router served directly, as
// tests do, behaves like the app. One hook per tree; a second call
// replaces the first, and nil removes it.
func (r *Router) PreRoute(fn func(*http.Request) (*http.Request, string)) {
	if fn == nil {
		r.root.preRoute.Store(nil)
		return
	}
	r.root.preRoute.Store(&fn)
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
//
// A path can match several patterns: /foo matches both "POST /foo" and
// "GET /{id}". So each registered method is resolved on its own, the way
// the real mux would dispatch it, and the route it lands on is
// gate-checked: a method whose route is gated answers 404 there (the mux
// never falls back to a less specific pattern), so it is not allowed.
// An empty result means the path exists but every method on it is gated
// off, which is a 404 by design (advertising Allow for a method that
// answers 404 would be a lie, and would leak the disabled module).
func (r *Router) allowedMethods(req *http.Request) []string {
	probeMux, methods := r.probe()
	if probeMux == nil {
		return nil
	}

	probe := req.Clone(req.Context())
	var allowed []string
	for _, m := range methods {
		probe.Method = m
		h, _ := probeMux.Handler(probe)
		// Anything other than a probeRoute is the mux's own 404/405 or a
		// synthesised redirect: the path has no route under m.
		if pr, ok := h.(probeRoute); ok && r.gateAllows(string(pr)) {
			allowed = append(allowed, m)
		}
	}
	return allowed
}

// probeRoute stands in for a registered route in the probe mux. Its value
// is the route's gate key, so a probe lookup names the route it hit.
type probeRoute string

func (probeRoute) ServeHTTP(http.ResponseWriter, *http.Request) {}

// probe returns the memoised probe mux, holding every registered
// "METHOD pattern" as the real mux does, and the sorted set of registered
// methods, rebuilding only when new routes have been registered since
// the last build.
//
// This used to be rebuilt from scratch on EVERY unmatched request:
// cloning the request and re-parsing plus tree-inserting every
// registered pattern. Against 300 routes that measured ~205us and 3704
// allocations versus 170ns for a matched request: a ~1200x
// amplification driven entirely by an attacker-supplied URL, and it ran
// AHEAD of the rate limiter (which lives in the middleware chain the
// fallback only enters afterwards). r.root.patterns is append-only, so
// its length is a sufficient cache version.
func (r *Router) probe() (*http.ServeMux, []string) {
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
	seen := make(map[string]bool)
	var methods []string
	for _, rt := range allPatterns {
		if !seen[rt.Method] {
			seen[rt.Method] = true
			methods = append(methods, rt.Method)
		}
		key := rt.Method + " " + rt.Pattern
		func() {
			// The real mux already accepted this pattern; a recover here
			// only guards a duplicate the real mux would also have refused.
			defer func() { _ = recover() }()
			probeMux.Handle(key, probeRoute(key))
		}()
	}
	// ServeMux answers HEAD on a GET pattern, so HEAD is probed too: it
	// resolves to the GET route and shares its gate, and on a path with no
	// GET it resolves to nothing and is not listed.
	if !seen[http.MethodHead] {
		methods = append(methods, http.MethodHead)
	}
	sort.Strings(methods)

	root.probeMux = probeMux
	root.probeMethods = methods
	root.probeN = n
	return probeMux, methods
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

// UseInner adds middleware that wraps every handler in the tree from the
// inside: it runs after every middleware added with Use, on this router,
// its parent or any Group, so it sees the context those middleware built
// (the authenticated user, the resolved tenant). Among themselves, inner
// middleware keep Use's order: the first added is the outermost of them.
// A call on a Group adds to the whole tree.
//
// The framework mounts its idempotency middleware here, so a replay is
// keyed on the tenant and user the app's own middleware resolved.
//
// Like Use, safe to call concurrently with in-flight ServeHTTP, and a
// call after a route was registered still wraps it.
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
	r.root.mu.RLock()
	inner := slices.Clone(r.root.inner)
	r.root.mu.RUnlock()
	for _, c := range slices.Backward(inner) {
		handler = c(handler)
	}
	chain := r.effectiveChain()
	for _, c := range slices.Backward(chain) {
		handler = c(handler)
	}
	return handler
}
