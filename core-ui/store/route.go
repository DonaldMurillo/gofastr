package store

// the route.* signal family.
//
// The route state (path, pattern, title, params, query) is owned by
// the server's router; these slices expose it to SSR bindings and to
// the client signal bus under reserved names:
//
//	route.path, route.pattern, route.title
//	route.params.<name>, route.query.<key>
//
// Every value is TEXT (a path, a param, a title can carry
// user-influenced characters); BindHTML refuses the family the same
// way the runtime's html mode refuses an untrusted write.
//
// Variant B (this commit): the values travel as ORDINARY page-scoped
// seed entries. The per-request values are seeded into the value bag
// (SeedRoute) so Bind stamps the live value at SSR time; SeedSplit
// includes every declared route.* name on every partial (absent
// params as ''), and the client's seed merge applies page-scoped keys
// through the NOTIFYING write path, so kept-layer bindings repaint.
// The merge writes keys one at a time, so a computed over two route
// values CAN observe a mid-merge mix — the comparison records it.

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// declareRoute declares one of the route family's own slices: the
// generic New path refuses every route.* name (validateName's
// reservation — the family is the router's snapshot, not user state),
// and this is the one caller that owns the prefix. The charset check
// still applies.
func declareRoute(name string) *Slice[string] {
	full := "route." + name
	validateNameChars(full)
	register(full, ScopePage, "")
	return &Slice[string]{name: full, scope: ScopePage, def: ""}
}

var (
	routePathSlice    = declareRoute("path")
	routePatternSlice = declareRoute("pattern")
	routeTitleSlice   = declareRoute("title")
)

// Route addresses the route.* signal family. Use it like a struct:
//
//	store.Route.Title.Bind(ctx, "span", nil)
//	store.Route.Param("id").Bind(ctx, "span", nil)
//
// Param and Query declare their slice on first use; identical
// re-declaration is idempotent.
var Route = struct {
	Path    *Slice[string]
	Pattern *Slice[string]
	Title   *Slice[string]
	Param   func(name string) *Slice[string]
	Query   func(key string) *Slice[string]
}{
	Path:    routePathSlice,
	Pattern: routePatternSlice,
	Title:   routeTitleSlice,
	Param:   func(name string) *Slice[string] { return routeParam(name) },
	Query:   func(key string) *Slice[string] { return routeQuery(key) },
}

// The method-style twin of the field struct (kept for callers that
// prefer it); both address the same slices.
type RouteSlices struct{}

// Path returns the route.path slice (the decoded request path).
func (RouteSlices) Path() *Slice[string] { return routePathSlice }

// Pattern returns the route.pattern slice (the matched route's
// canonical pattern, e.g. "/inbox/:id").
func (RouteSlices) Pattern() *Slice[string] { return routePatternSlice }

// Title returns the route.title slice (the effective post-Load title).
func (RouteSlices) Title() *Slice[string] { return routeTitleSlice }

var (
	routeParamMu    sync.Mutex
	routeParamSli   = map[string]*Slice[string]{}
	routeQuerySli   = map[string]*Slice[string]{}
	routeParamNames []string
	routeQueryNames []string
)

// Param declares and returns the route.params.<name> slice.
func (RouteSlices) Param(name string) *Slice[string] {
	routeParamMu.Lock()
	defer routeParamMu.Unlock()
	if sl, ok := routeParamSli[name]; ok {
		return sl
	}
	sl := declareRoute("params." + name)
	routeParamSli[name] = sl
	routeParamNames = append(routeParamNames, name)
	return sl
}

// Query declares and returns the route.query.<key> slice.
func (RouteSlices) Query(key string) *Slice[string] {
	routeParamMu.Lock()
	defer routeParamMu.Unlock()
	if sl, ok := routeQuerySli[key]; ok {
		return sl
	}
	sl := declareRoute("query." + key)
	routeQuerySli[key] = sl
	routeQueryNames = append(routeQueryNames, key)
	return sl
}

func routeParam(name string) *Slice[string] { return RouteSlices{}.Param(name) }

func routeQuery(key string) *Slice[string] { return RouteSlices{}.Query(key) }

// IsRouteName reports whether a signal name belongs to the family.
func IsRouteName(name string) bool {
	return strings.HasPrefix(name, "route.")
}

// RouteNames returns every declared route.* slice name, sorted and
// deduplicated. The partial seed under variant B includes all of them
// on every navigation (absent params as ”) so values from a previous
// route never survive.
func RouteNames() []string {
	routeParamMu.Lock()
	names := make([]string, 0, 3+len(routeParamNames)+len(routeQueryNames))
	names = append(names, "route.path", "route.pattern", "route.title")
	for _, n := range routeParamNames {
		names = append(names, "route.params."+n)
	}
	for _, k := range routeQueryNames {
		names = append(names, "route.query."+k)
	}
	routeParamMu.Unlock()
	sort.Strings(names)
	return names
}

// SeedRoute writes the route snapshot into the request value bag so
// Route.*.Bind stamps the live values at SSR time.
//
// : every resolved param and query value is written under its
// wire name WHETHER OR NOT a slice for it was declared before this
// render, so a binding created during the render (a layout or screen
// calling Param/Query for the first time) resolves its value on the
// FIRST render of a route carrying it. Declared names still seed ”
// when absent, so a previous route's value never survives into the
// partial seed. No-op outside a value-bag context (direct renders
// fall back to the declared defaults; the design's core-ui/route
// carrier closes that gap).
func SeedRoute(ctx context.Context, path, pattern, title string, params, query map[string]string) {
	routePathSlice.Seed(ctx, path)
	routePatternSlice.Seed(ctx, pattern)
	routeTitleSlice.Seed(ctx, title)
	for k, v := range params {
		seedBagValue(ctx, "route.params."+k, v)
	}
	for k, v := range query {
		seedBagValue(ctx, "route.query."+k, v)
	}
	for _, n := range RouteNames() {
		switch {
		case strings.HasPrefix(n, "route.params."):
			Route.Param(strings.TrimPrefix(n, "route.params.")).Seed(ctx, params[strings.TrimPrefix(n, "route.params.")])
		case strings.HasPrefix(n, "route.query."):
			Route.Query(strings.TrimPrefix(n, "route.query.")).Seed(ctx, query[strings.TrimPrefix(n, "route.query.")])
		}
	}
}

// seedBagValue writes one raw name→value into the request bag without
// a slice (the first-render rule above); a name with no slice
// declaration is inert until a Bind during this render declares one.
func seedBagValue(ctx context.Context, name string, v any) {
	if b := valuesFrom(ctx); b != nil {
		b.mu.Lock()
		b.m[name] = v
		b.mu.Unlock()
	}
}

// seedRouteQueryFrom builds the first-value-per-key query map the
// router snapshot carries (url.Values.Get parity).
func SeedRouteQueryFrom(q map[string][]string) map[string]string {
	if len(q) == 0 {
		return nil
	}
	out := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
