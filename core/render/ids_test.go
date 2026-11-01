package render

import (
	"context"
	"strings"
	"testing"
)

func TestUniqueIDSuffixesRepeats(t *testing.T) {
	ctx := WithIDScope(context.Background())
	ReserveID(ctx, "plans")
	got := []string{
		UniqueID(ctx, "features"),
		UniqueID(ctx, "features"),
		UniqueID(ctx, "features-3"),
		UniqueID(ctx, "features"),
		UniqueID(ctx, "plans"),
	}
	want := []string{"features", "features-2", "features-3", "features-4", "plans-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUniqueIDWithoutScopeKeepsID(t *testing.T) {
	ctx := context.Background()
	if a, b := UniqueID(ctx, "x"), UniqueID(ctx, "x"); a != "x" || b != "x" {
		t.Errorf("no scope: %q, %q", a, b)
	}
	var none context.Context // a component config whose Ctx is unset
	if got := UniqueID(none, "x"); got != "x" {
		t.Errorf("nil ctx: %q", got)
	}
}

func TestRegionBatchSettlesInOrder(t *testing.T) {
	ctx := WithIDScope(context.Background())
	UniqueID(ctx, "a") // the screen took "a" before the regions
	render := func(rctx context.Context) HTML {
		x, y := UniqueID(rctx, "a"), UniqueID(rctx, "b")
		ReserveID(rctx, "keep")
		return HTML(`<h2 id="` + x + `"></h2><p aria-labelledby="` + x + `-title" id="` + y + `"></p>`)
	}
	b := NewRegionBatch(ctx, 2)
	// Region 1 renders first, region 0 second: Settle order decides.
	h1 := render(b.Context(1))
	h0 := render(b.Context(0))
	got := []HTML{b.Settle(0)(h0), b.Settle(1)(h1)}
	want := []HTML{
		`<h2 id="a-2"></h2><p aria-labelledby="a-2-title" id="b"></p>`,
		`<h2 id="a-3"></h2><p aria-labelledby="a-3-title" id="b-2"></p>`,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("region %d = %s, want %s", i, got[i], want[i])
		}
	}
	if got := UniqueID(ctx, "keep"); got != "keep-2" {
		t.Errorf("a region's explicit id is not taken on the page: %q", got)
	}
}

func TestRegionBatchNestsInARegion(t *testing.T) {
	ctx := WithIDScope(context.Background())
	outer := NewRegionBatch(ctx, 1)
	octx := outer.Context(0)
	inner := NewRegionBatch(octx, 2)
	h0, h1 := HTML(UniqueID(inner.Context(0), "a")), HTML(UniqueID(inner.Context(1), "a"))
	h := inner.Settle(0)(h0) + " " + inner.Settle(1)(h1)
	if got := outer.Settle(0)(h); got != "a a-2" {
		t.Errorf("nested = %q, want %q", got, "a a-2")
	}
}

func TestRegionBatchUnsafeIDKeepsBytes(t *testing.T) {
	ctx := WithIDScope(context.Background())
	b := NewRegionBatch(ctx, 1)
	rctx := b.Context(0)
	h := HTML(UniqueID(rctx, "a.b") + " " + UniqueID(rctx, "a.b"))
	if got := b.Settle(0)(h); got != "a.b a.b-2" {
		t.Errorf("unsafe ids = %q", got)
	}
}

func TestRegionBatchWithoutScope(t *testing.T) {
	b := NewRegionBatch(context.Background(), 2)
	if got := UniqueID(b.Context(1), "a"); got != "a" {
		t.Errorf("no scope: %q", got)
	}
	if got := b.Settle(1)("x"); got != "x" {
		t.Errorf("settle: %q", got)
	}
}

func TestUniqueIDTakesTheFamily(t *testing.T) {
	ctx := WithIDScope(context.Background())
	got := []string{
		UniqueID(ctx, "features", "-title"),
		UniqueID(ctx, "features-title", "-title"),
		UniqueID(ctx, "features"),
	}
	want := []string{"features", "features-title-2", "features-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
	ReserveID(ctx, "plans", "-title")
	if got := UniqueID(ctx, "plans-title"); got != "plans-title-2" {
		t.Errorf("an explicit id's family is taken: %q", got)
	}
}

func TestRegionExplicitIDWins(t *testing.T) {
	ctx := WithIDScope(context.Background())
	b := NewRegionBatch(ctx, 2)
	// Region 0 derives "features" before region 1 (and region 0 itself)
	// names it explicitly.
	h0 := HTML(UniqueID(b.Context(0), "features", "-title"))
	ReserveID(b.Context(0), "plans")
	ReserveID(b.Context(1), "features")
	h1 := HTML(UniqueID(b.Context(1), "plans"))
	got := []HTML{b.Settle(0)(h0), b.Settle(1)(h1)}
	if got[0] != "features-2" || got[1] != "plans-2" {
		t.Errorf("derived ids = %v, want [features-2 plans-2]", got)
	}
}

func TestUnsafeIDsSettleAcrossRegions(t *testing.T) {
	ctx := WithIDScope(context.Background())
	b := NewRegionBatch(ctx, 3)
	// Region 1 renders first; both derive a tab strip named "cart.view".
	h1 := HTML(UniqueID(b.Context(1), "cart.view", "-tab-0") + "-tab-0")
	h0 := HTML(UniqueID(b.Context(0), "cart.view", "-tab-0") + "-tab-0")
	// Region 2 names "x.y" explicitly after region 0 derived it.
	h2 := HTML(UniqueID(b.Context(0), "x.y"))
	ReserveID(b.Context(2), "x.y")
	got := []HTML{b.Settle(0)(h0 + " " + h2), b.Settle(1)(h1)}
	want := []HTML{"cart.view-tab-0 x.y-2", "cart.view-2-tab-0"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("region %d = %q, want %q", i, got[i], want[i])
		}
	}
	// A region nested in a region settles through the outer one.
	ob := NewRegionBatch(ctx, 1)
	ib := NewRegionBatch(ob.Context(0), 1)
	ih := HTML(UniqueID(ib.Context(0), "cart.view"))
	ih = ib.Settle(0)(ih)
	if got := ob.Settle(0)(ih); got != "cart.view-3" {
		t.Errorf("nested unsafe id = %q, want cart.view-3", got)
	}
	// In a namespace the unsafe id takes the namespace as well.
	nb := NewRegionBatch(WithIDNamespace(ctx, "cart"), 1)
	h := HTML(UniqueID(nb.Context(0), "a.b"))
	if got := nb.Settle(0)(h); got != "cart--a.b" {
		t.Errorf("namespaced unsafe id = %q, want cart--a.b", got)
	}
}

func TestNamespaceBoundaryIsUnambiguous(t *testing.T) {
	page := WithIDScope(context.Background())
	a := UniqueID(WithIDNamespace(page, "a"), "b--c", "-tab-0")
	ab := UniqueID(WithIDNamespace(page, "a--b"), "c", "-tab-0")
	if a == ab {
		t.Errorf("islands a and a--b both derived %q", a)
	}
	for _, ns := range []string{"a-", "-a", "a_b", "A"} {
		got := UniqueID(WithIDNamespace(page, ns), "x")
		if strings.Contains(strings.TrimSuffix(got, "--x"), "--") || strings.HasSuffix(strings.TrimSuffix(got, "--x"), "-") {
			t.Errorf("namespace %q gives %q: the boundary is not the first --", ns, got)
		}
	}
	if got := UniqueID(WithIDNamespace(page, "my-cart"), "features"); got != "my-cart--features" {
		t.Errorf("a plain namespace reads %q, want my-cart--features", got)
	}
}

func TestNamespaceIsTokenSafe(t *testing.T) {
	ns := `x" onmouseover="alert(1)" data-x="`
	page := WithIDScope(context.Background())
	if got := UniqueID(WithIDNamespace(page, ns), "f"); !tokenSafe(got, nil) {
		t.Errorf("namespaced id %q carries unsafe bytes", got)
	}
	b := NewRegionBatch(WithIDNamespace(page, ns), 1)
	h := HTML(`<h2 id="` + UniqueID(b.Context(0), "features") + `">`)
	got := string(b.Settle(0)(h))
	id := strings.TrimSuffix(strings.TrimPrefix(got, `<h2 id="`), `">`)
	if !tokenSafe(id, nil) {
		t.Errorf("settled id %q carries unsafe bytes: %s", id, got)
	}
}
