package render

import (
	"context"
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
