package store

import (
	"context"
	"sort"
	"strings"
	"sync"
)

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

// Route addresses the route.* signal family; Param and Query declare their slice on first use.
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
	Param:   routeParam,
	Query:   routeQuery,
}

var (
	routeParamMu    sync.Mutex
	routeParamSli   = map[string]*Slice[string]{}
	routeQuerySli   = map[string]*Slice[string]{}
	routeParamNames []string
	routeQueryNames []string
)

func routeParam(name string) *Slice[string] {
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

func routeQuery(key string) *Slice[string] {
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

// IsRouteName reports whether a signal name belongs to the family.
func IsRouteName(name string) bool {
	return strings.HasPrefix(name, "route.")
}

// RouteNames returns every declared route.* slice name, sorted.
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

// SeedRoute writes the router's snapshot into the request's value bag.
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

func seedBagValue(ctx context.Context, name string, v any) {
	if b := valuesFrom(ctx); b != nil {
		b.mu.Lock()
		b.m[name] = v
		b.mu.Unlock()
	}
}

// SeedRouteQueryFrom keeps the first value of each query key.
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
