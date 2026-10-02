package store

import (
	"context"
	"strings"
	"testing"
)

// TestRouteSeedDueGatesTheWriters pins the Opt-in decision's route.*
// arm (docs/DESIGN-layout-outlets.md "### Opt-in"): the seed writers
// (SeedFor / SeedSplit) include the declared route.* names only when
// the render read a route slice, the chain carries a RouteArea
// (MarkRouteArea), or a computed names one in a deps list the
// reference scan cannot decompose. A marketing page or a blog ships
// no route.* seed at all.
func TestRouteSeedDueGatesTheWriters(t *testing.T) {
	plainHTML := `<span data-fui-signal="cart.n">0</span>`

	ctx := WithValues(context.Background())
	for k := range SeedFor(ctx, plainHTML) {
		if strings.HasPrefix(k, "route.") {
			t.Fatalf("seed carries %q with no read, no area, no computed dep", k)
		}
	}
	page, global := SeedSplit(ctx, plainHTML)
	for k := range page {
		if strings.HasPrefix(k, "route.") {
			t.Fatalf("partial seed carries %q with no read, no area, no computed dep", k)
		}
	}
	if len(global) > 0 && strings.HasPrefix(global["x"].(string), "route.") {
		t.Fatal("unreachable")
	}

	// The computed-deps mention.
	htmlComputed := `<span data-fui-computed="crumbs" data-fui-computed-deps="route.path,cart.n">x</span>`
	if _, ok := SeedFor(ctx, htmlComputed)["route.path"]; !ok {
		t.Error("seed missing route.path for a computed naming it in its deps list")
	}

	// The read tracker: a Bind during the render.
	fresh := WithValues(context.Background())
	_ = Route.Title.Bind(fresh, "span", nil)
	if _, ok := SeedFor(fresh, plainHTML)["route.title"]; !ok {
		t.Error("seed missing route.title after a Bind read it")
	}

	// The chain-area mark.
	fresh2 := WithValues(context.Background())
	MarkRouteArea(fresh2)
	if _, ok := SeedFor(fresh2, plainHTML)["route.path"]; !ok {
		t.Error("seed missing route.path after MarkRouteArea")
	}
}
