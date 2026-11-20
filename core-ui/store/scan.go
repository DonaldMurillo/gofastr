package store

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/internal/attrscan"
)

var refAttrs = []string{"signal-set", "signal-inc", "signal-toggle", "signal", "computed"}

func refNameByte(c byte) bool { return c != '"' && c != ':' }

// ScanReferenced returns the sorted slice names the rendered html's signal attributes reference.
func ScanReferenced(html string) []string {
	seen := map[string]bool{}
	for n := range attrscan.Values(html, refAttrs, refNameByte, false) {
		seen[n] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ResolveSeed returns each declared name's request value or default, ready for JSON.
func ResolveSeed(ctx context.Context, names []string) map[string]any {
	bag := valuesFrom(ctx)
	out := make(map[string]any, len(names))
	regMu.RLock()
	defer regMu.RUnlock()
	for _, n := range names {
		d, ok := declRegistry[n]
		if !ok || d.computed {
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
		out[n] = settleSeed(ctx, val)
	}
	return out
}

func settleSeed(ctx context.Context, v any) any {
	if !render.HasIDScope(ctx) {
		return v
	}
	if s, ok := v.(string); ok {
		return render.SettleIDs(ctx, s)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return v
	}
	if rv.Kind() == reflect.String {
		s := rv.String()
		if t := render.SettleIDs(ctx, s); t != s {
			return reflect.ValueOf(t).Convert(rv.Type()).Interface()
		}
		return v
	}
	j, err := json.Marshal(v)
	if err != nil {
		return v
	}
	if t := render.SettleIDs(ctx, string(j)); t != string(j) {
		return json.RawMessage(t)
	}
	return v
}

// SeedFor resolves the seed for a full page: referenced, global and, when due, route slices.
func SeedFor(ctx context.Context, html string) map[string]any {
	names := ScanReferenced(html)
	names = append(names, GlobalNames()...)
	if routeSeedDue(ctx, html) {
		names = append(names, RouteNames()...)
	}
	return ResolveSeed(ctx, names)
}

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

var routeDepRe = regexp.MustCompile(`data-cui-computed-deps="(?:[^"]*?,)?route\.`)

// MarkRouteArea marks the request's chain as carrying a RouteArea, so the route family seeds.
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

// SeedSplit is SeedFor for a partial render, split into page and global values.
func SeedSplit(ctx context.Context, html string) (page, global map[string]any) {
	names := append(ScanReferenced(html), GlobalNames()...)
	if routeSeedDue(ctx, html) {
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
