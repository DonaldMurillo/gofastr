package webmcp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// HandleOption customizes one Handle registration.
type HandleOption func(*handleConfig)

type handleConfig struct {
	middleware []router.Middleware
}

// WithHTTPMiddleware wraps the route handler Handle registers with mw,
// outermost first (the same order Router.Use applies). This is where
// authorization belongs: the middleware sits next to the tool
// declaration it protects instead of in a distant router block, so a
// tool without an auth wrapper reads as a decision, not an oversight.
//
// The middleware wraps only this tool's route. It never gates the
// bridge script or manifest, and the WebMCP marker header never grants
// anything: an agent's call is authenticated exactly like the same
// fetch made by hand.
func WithHTTPMiddleware(mw ...router.Middleware) HandleOption {
	return func(c *handleConfig) { c.middleware = append(c.middleware, mw...) }
}

// Handle declares t AND binds its endpoint in one call: the tool is
// added to the manifest exactly as Register would, and handler is
// registered on rt at t's method and path. One declaration therefore
// produces the manifest entry and the route together — the two can no
// longer drift apart, and a conditional registration (an `if` around
// the Handle call) removes both at once.
//
// The route pattern is the path portion of t.Path; a query string in
// the declaration is forwarded to the browser (the bridge bakes it
// into every fetch) but is not part of the pattern the router matches.
// The handler receives the query through r.URL.Query as usual, and the
// route stays callable by hand — WebMCP calls differ only in carrying
// the X-Gofastr-WebMCP marker header, which attributes but never
// authorizes.
//
// Handle fails cleanly, registering neither manifest entry nor route,
// for every reason Register does, for a duplicate method+pattern
// already on rt (checked before anything is registered, so the router
// never panics mid-wiring), and after Mount has frozen the tool set.
// On a grouped router the route lands under the group prefix, exactly
// as an explicit rt.Handle call would.
func (h *Host) Handle(rt *router.Router, t Tool, handler http.Handler, opts ...HandleOption) error {
	var cfg handleConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	declared := t
	t, class, err := validateTool(t)
	if err != nil {
		h.emitRegister(declared, class)
		return err
	}
	pattern := pathPortion(t.Path)
	h.mu.Lock()
	switch {
	case h.mounted:
		class = "after_mount"
		err = fmt.Errorf("webmcp: Handle(%q) refused: Mount already froze the tool set; register every tool before Mount", t.Name)
	case h.names[t.Name]:
		class = "duplicate_name"
		err = fmt.Errorf("webmcp: Handle: duplicate tool name %q (the browser refuses duplicate registrations)", t.Name)
	}
	if err == nil {
		// Pre-flight the route pattern: the router panics on conflicting
		// registrations, so a collision (a second Handle onto the same
		// method+pattern, or an app route already there) must surface as a
		// returned error BEFORE anything is registered, keeping "a failed
		// Handle leaves nothing behind" true. Routes() reports full
		// (prefix-joined) patterns.
		for _, route := range rt.Routes() {
			if route.Method == t.Method && route.Pattern == full(rt, pattern) {
				class = "route_conflict"
				err = fmt.Errorf("webmcp: Handle(%q): %s %s is already registered on this router; one endpoint cannot serve two tool declarations (and the manifest must not advertise a path the router would panic on)", t.Name, t.Method, full(rt, pattern))
				break
			}
		}
	}
	if err == nil {
		// Compose outermost-first, matching Router.Use semantics: the first
		// middleware listed runs first on the way in. The observer wrapper
		// goes outermost of all so it also sees middleware outcomes (a 401
		// from an auth wrapper is an invocation the observer should report).
		final := handler
		for i := len(cfg.middleware) - 1; i >= 0; i-- {
			final = cfg.middleware[i](final)
		}
		if h.observer != nil {
			final = h.observeHandler(t, final)
		}
		// The pre-flight above catches identical patterns; net/http's
		// ServeMux also refuses two patterns that overlap without one
		// being more specific ("/api/{id}" against "/api/{name}"), and
		// it reports that by panicking inside rt.Handle. Convert the
		// panic into the returned error the contract promises, and
		// never let it escape with h.mu held.
		if perr := registerRoute(rt, t.Method, pattern, final); perr != nil {
			class = "route_conflict"
			err = fmt.Errorf("webmcp: Handle(%q): %s %s conflicts with a route already on this router: %v", t.Name, t.Method, full(rt, pattern), perr)
		} else {
			h.names[t.Name] = true
			h.tools = append(h.tools, t)
		}
	}
	h.mu.Unlock()
	if err != nil {
		h.emitRegister(t, class)
	}
	return err
}

// pathPortion returns p without its query string. validToolPath has
// already rejected fragments and control bytes, so '?' is the only
// separator that can follow the path.
func pathPortion(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// full is the prefix-joined pattern the router reports for a registration.
func full(rt *router.Router, pattern string) string { return rt.Prefix() + pattern }

// registerRoute wraps rt.Handle so a ServeMux registration panic
// (overlapping wildcard patterns) becomes an error.
func registerRoute(rt *router.Router, method, pattern string, h http.Handler) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	rt.Handle(method, pattern, h)
	return nil
}
