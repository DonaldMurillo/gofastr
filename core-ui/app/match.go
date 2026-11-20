package app

import (
	"context"
	"maps"
)

// Match is a read-only snapshot of one request path's route resolution.
type Match struct {
	screenID string
	path     string
	params   map[string]string
}

func newMatch(screenID, path string, params map[string]string) Match {
	path = decodeRouteSeg(path)
	if len(params) == 0 {
		return Match{screenID: screenID, path: path}
	}
	return Match{screenID: screenID, path: path, params: maps.Clone(params)}
}

// ScreenID returns the matched route pattern in the router's canonical ":param" form.
func (m Match) ScreenID() string { return m.screenID }

// Path returns the concrete request path the match was resolved from.
func (m Match) Path() string { return m.path }

// Param returns the value of the named dynamic route parameter, or "" when the route does not declare it.
func (m Match) Param(name string) string { return m.params[name] }

// MatchFor resolves path against the router and returns the immutable [Match] snapshot.
func (r *Router) MatchFor(path string) (Match, bool) {
	screen, params, ok := r.Resolve(path)
	if !ok {
		return Match{}, false
	}
	return newMatch(screen.Path, path, params), true
}

type matchContextKey struct{}

// WithMatch returns a context carrying m for [MatchFromContext].
func WithMatch(ctx context.Context, m Match) context.Context {
	return context.WithValue(ctx, matchContextKey{}, m)
}

// MatchFromContext returns the route match the authoritative router produced for this request.
func MatchFromContext(ctx context.Context) (Match, bool) {
	if ctx == nil {
		return Match{}, false
	}
	m, ok := ctx.Value(matchContextKey{}).(Match)
	return m, ok
}
