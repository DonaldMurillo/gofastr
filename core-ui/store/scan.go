package store

import (
	"context"
	"regexp"
	"sort"
)

// refRe matches a signal-referencing data-cui-* attribute in an opening
// tag and captures the bare slice name (the part before any ":value").
// The leading [\s/] boundary restricts matches to attribute positions
// (preceded by whitespace or a self-closing slash), so a literal mention
// inside <pre>/<code>/text content never registers a false reference,
// mirrors registry.markerRe in core-ui/registry/render.go.
var refRe = regexp.MustCompile(`[\s/]data-cui-(?:signal-set|signal-inc|signal-toggle|signal|computed)="([^":]+)`)

// ScanReferenced returns the unique, sorted slice names referenced by
// signal/computed attributes in the rendered HTML.
func ScanReferenced(html string) []string {
	matches := refRe.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		if len(m) >= 2 && m[1] != "" {
			seen[m[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ResolveSeed builds the seed map for the given names: each registered
// name resolves to its per-request value (if a producer seeded one) or
// its declared default. Unregistered names (hand-written attrs with no
// declaration) are skipped, there is nothing to seed. The returned
// values are raw Go values ready for JSON marshaling by the host.
func ResolveSeed(ctx context.Context, names []string) map[string]any {
	bag := valuesFrom(ctx)
	out := make(map[string]any, len(names))
	regMu.RLock()
	defer regMu.RUnlock()
	for _, n := range names {
		d, ok := declRegistry[n]
		if !ok || d.computed {
			// Unregistered names have no default; computed slices are
			// derived client-side and never seeded.
			continue
		}
		val := d.def
		if bag != nil {
			bag.mu.Lock()
			if rv, ok := bag.m[n]; ok {
				val = rv
			}
			bag.mu.Unlock()
		}
		out[n] = val
	}
	return out
}

// SeedFor is the host convenience: scan the page for referenced names,
// add all app-global names, and resolve the combined seed in one call.
//
// every DECLARED route.* name joins the
// full-page seed as well. A route value can be named ONLY inside a
// data-cui-computed-deps list (route.path in a breadcrumb computed),
// which the scan reads as one comma-joined name it cannot decompose;
// without the unconditional inclusion the boot recompute of such a
// computed finds no value and clobbers the correct SSR stamp.
func SeedFor(ctx context.Context, html string) map[string]any {
	names := ScanReferenced(html)
	names = append(names, GlobalNames()...)
	if routeSeedDue(ctx, html) {
		// Every DECLARED route.* name joins the full-page seed as well.
		// A route value can be named ONLY inside a
		// data-cui-computed-deps list (route.path in a breadcrumb
		// computed), which the scan reads as one comma-joined name it
		// cannot decompose; without the unconditional inclusion the
		// boot recompute of such a computed finds no value and
		// clobbers the correct SSR stamp.
		names = append(names, RouteNames()...)
	}
	return ResolveSeed(ctx, names)
}

// routeSeedDue is the wire gate for the route.* family (the opt-in
// decision, 2026-09-26): the names join a seed only when the render
// READ a route slice (the bag tracker Bind/BindAttr set), the chain
// carries a RouteArea (MarkRouteArea, whose markup may bind anything),
// or the html names one inside a data-cui-computed-deps list the
// reference scan cannot decompose. A render that did none of that — a
// marketing page, a blog — seeds no route.* at all.
func routeSeedDue(ctx context.Context, html string) bool {
	if b := valuesFrom(ctx); b != nil {
		b.mu.Lock()
		due := b.routeRead || b.routeArea
		b.mu.Unlock()
		if due {
			return true
		}
	}
	return routeDepRe.MatchString(html)
}

// routeDepRe matches a computed's dependency list that names a route.*
// slice. The attribute is comma-joined signal names (applyComputed),
// so "a,route.path" matches and "aroute.path" does not: the prefix is
// anchored on a comma or the opening quote.
var routeDepRe = regexp.MustCompile(`data-cui-computed-deps="(?:[^"]*?,)?route\.`)

// MarkRouteArea marks the request's chain as carrying a RouteArea: the
// area re-renders on every navigation the layer survives and its
// markup can bind any route.* slice, so the family seeds.
func MarkRouteArea(ctx context.Context) {
	if b := valuesFrom(ctx); b != nil {
		b.mu.Lock()
		b.routeArea = true
		b.mu.Unlock()
	}
}

// ScopeOf returns the declared scope of a slice (ScopePage if unknown).
func ScopeOf(name string) Scope {
	regMu.RLock()
	defer regMu.RUnlock()
	if d, ok := declRegistry[name]; ok {
		return d.scope
	}
	return ScopePage
}

// SeedSplit resolves the seed for a partial (SPA-nav) render, split by
// scope. The client merges page-scoped values unconditionally (fresh
// page) but only seeds a global the first time it is seen (preserving
// any value the user mutated on a previous page).
//
// every DECLARED route.* name
// joins the seed unconditionally. A partial's scanned HTML cannot see
// a binding in a KEPT layer (the route-bound header of a kept shell
// travels in no payload), so the scan alone would leave those names
// unseeded and the kept bindings stale; and because SeedRoute writes
// absent params as ”, the unconditional page-scoped merge also erases
// the previous route's values on the client.
func SeedSplit(ctx context.Context, html string) (page, global map[string]any) {
	names := append(ScanReferenced(html), GlobalNames()...)
	if routeSeedDue(ctx, html) {
		//: every DECLARED route.* name joins the seed.
		// A partial's scanned HTML cannot see a binding in a KEPT
		// layer (the route-bound header of a kept shell travels in no
		// payload), so the scan alone would leave those names unseeded
		// and the kept bindings stale; and because SeedRoute writes
		// absent params as '', the unconditional page-scoped merge
		// also erases the previous route's values on the client.
		names = append(names, RouteNames()...)
	}
	all := ResolveSeed(ctx, names)
	page = map[string]any{}
	global = map[string]any{}
	for n, v := range all {
		if ScopeOf(n) == ScopeGlobal {
			global[n] = v
		} else {
			page[n] = v
		}
	}
	return page, global
}
