package store

import (
	"context"
	"regexp"
	"sort"
)

// refRe matches a signal-referencing data-fui-* attribute in an opening
// tag and captures the bare slice name (the part before any ":value").
// The leading [\s/] boundary restricts matches to attribute positions
// (preceded by whitespace or a self-closing slash), so a literal mention
// inside <pre>/<code>/text content never registers a false reference —
// mirrors registry.markerRe in core-ui/registry/render.go.
var refRe = regexp.MustCompile(`[\s/]data-fui-(?:signal-set|signal-inc|signal-toggle|signal|computed)="([^":]+)`)

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
// declaration) are skipped — there is nothing to seed. The returned
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
func SeedFor(ctx context.Context, html string) map[string]any {
	names := ScanReferenced(html)
	names = append(names, GlobalNames()...)
	return ResolveSeed(ctx, names)
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
func SeedSplit(ctx context.Context, html string) (page, global map[string]any) {
	names := append(ScanReferenced(html), GlobalNames()...)
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
