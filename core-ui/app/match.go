package app

import (
	"context"
	"maps"
)

// Match is a read-only snapshot of the route resolution for one request
// path: the registered route pattern (ScreenID), the concrete request
// path, and the dynamic route parameters the authoritative screen
// router extracted. Middleware that guards screen routes reads it via
// [MatchFromContext] instead of re-parsing the path: the parameter
// values are the same ones the render pipeline injects through
// SetParams, including Router.Resolve's trailing-slash tolerance
// ("/session/42/" and "/session/42" both match "/session/:sessionId").
//
// A Match is immutable. Param only reads, and no accessor exposes the
// underlying map, so middleware can hold one Match for the whole
// request without copying. The zero value carries no screen and no
// parameters; MatchFromContext reports presence.
type Match struct {
	screenID string
	path     string
	params   map[string]string
}

// newMatch builds a Match with its own copy of params, so later
// mutation of the source map cannot change what a holder already read.
// Resolve returns a fresh map per call, but the copy keeps the
// immutability contract from depending on that fact.
func newMatch(screenID, path string, params map[string]string) Match {
	if len(params) == 0 {
		return Match{screenID: screenID, path: path}
	}
	return Match{screenID: screenID, path: path, params: maps.Clone(params)}
}

// ScreenID returns the registered route pattern that matched, in the
// router's canonical ":param" form (e.g. "/session/:sessionId"). It
// identifies the screen, not the concrete URL; use [Match.Path] for
// the URL that was requested.
func (m Match) ScreenID() string { return m.screenID }

// Path returns the concrete request path the match was resolved from.
func (m Match) Path() string { return m.path }

// Param returns the value of the named dynamic route parameter, or ""
// when the route does not declare it. Values are the raw matched path
// text, exactly what the screen's SetParams receives for the same
// request.
func (m Match) Param(name string) string { return m.params[name] }

// MatchFor resolves path against the router and returns the immutable
// [Match] snapshot. ok is false when the path matches no screen:
// middleware should treat that as "not a screen route" and let the
// request fall through, so unknown paths keep their truthful 404.
func (r *Router) MatchFor(path string) (Match, bool) {
	screen, params, ok := r.Resolve(path)
	if !ok {
		return Match{}, false
	}
	return newMatch(screen.Path, path, params), true
}

// matchContextKey is the unexported type used to store a Match on a
// context.Context. The host (or the host-provided middleware, see
// uihost.UIHost.RouteMatchMiddleware) installs it before application
// middleware that guards screen routes.
type matchContextKey struct{}

// WithMatch returns a context carrying m for [MatchFromContext]. The
// host populates it; app code reads it.
func WithMatch(ctx context.Context, m Match) context.Context {
	return context.WithValue(ctx, matchContextKey{}, m)
}

// MatchFromContext returns the route match the authoritative router
// produced for this request. ok is false when the request carries no
// match: no middleware populated one, or the path matches no screen.
func MatchFromContext(ctx context.Context) (Match, bool) {
	if ctx == nil {
		return Match{}, false
	}
	m, ok := ctx.Value(matchContextKey{}).(Match)
	return m, ok
}
