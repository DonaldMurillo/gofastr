package store

import (
	"context"
	"testing"
)

func TestSeedSplitSeparatesGlobals(t *testing.T) {
	resetForTest()
	s := New("split")
	s.Int("page", 1)
	s.Int("cart", 2).Global()
	ctx := WithValues(context.Background())
	html := `<span data-cui-signal="split.page"></span>`
	if got := SeedFor(ctx, html); got["split.page"] != 1 || got["split.cart"] != 2 {
		t.Fatalf("SeedFor = %v, want the referenced and the global slice", got)
	}
	page, global := SeedSplit(ctx, html)
	if _, ok := page["split.cart"]; ok || page["split.page"] != 1 {
		t.Fatalf("page = %v", page)
	}
	if _, ok := global["split.page"]; ok || global["split.cart"] != 2 {
		t.Fatalf("global = %v", global)
	}
}

func TestRouteDepMustStartAName(t *testing.T) {
	resetForTest()
	ctx := WithValues(context.Background())
	for _, html := range []string{
		`<i data-cui-computed-deps="aroute.path"></i>`,
		`<i data-cui-computed-deps="a,xroute.path"></i>`,
	} {
		if _, ok := SeedFor(ctx, html)["route.path"]; ok {
			t.Errorf("%s seeded route.path", html)
		}
	}
	if _, ok := SeedFor(ctx, `<i data-cui-computed-deps="a,route.path"></i>`)["route.path"]; !ok {
		t.Error("a route dep after a comma did not seed route.path")
	}
}

func TestSeedRouteSeedsUndeclaredParams(t *testing.T) {
	ctx := WithValues(context.Background())
	SeedRoute(ctx, "/x", "/x", "X", map[string]string{"fresh": "7"}, nil)
	if got := Route.Param("fresh").Value(ctx); got != "7" {
		t.Fatalf("a param declared during the render = %q, want 7", got)
	}
	if got := SeedRouteQueryFrom(map[string][]string{"q": {"a", "b"}, "e": nil}); len(got) != 1 || got["q"] != "a" {
		t.Fatalf("SeedRouteQueryFrom = %v, want the first value per key", got)
	}
}
