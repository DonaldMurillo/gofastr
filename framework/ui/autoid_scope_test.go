package ui

import (
	"context"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

var firstID = regexp.MustCompile(`\sid="([^"]*)"`)

// An auto id in an island is the same on the page and in the RPC
// answer: it comes from the id scope, not a process-wide counter.
func TestAutoIDsMatchInAnIsland(t *testing.T) {
	slides := []CarouselSlide{{Content: render.Text("a")}}
	comps := map[string]func(context.Context) render.HTML{
		"carousel": func(ctx context.Context) render.HTML {
			return Carousel(CarouselConfig{Label: "Tour", Slides: slides, Ctx: ctx})
		},
		"repeater": func(ctx context.Context) render.HTML {
			return Repeater(RepeaterConfig{Name: "rows", Label: "Rows", Ctx: ctx,
				Template: func(int) render.HTML { return render.Text("row") }})
		},
		"counter": func(ctx context.Context) render.HTML {
			return Counter(CounterConfig{SignalName: "n", Ctx: ctx})
		},
	}
	for name, comp := range comps {
		page := render.WithIDScope(context.Background())
		comp(page) // the same component above the island takes an id first
		ssr := firstID.FindStringSubmatch(string(comp(render.WithIDNamespace(page, "cart"))))
		rpc := firstID.FindStringSubmatch(string(comp(render.WithIDNamespace(context.Background(), "cart"))))
		if ssr == nil || rpc == nil || ssr[1] != rpc[1] {
			t.Errorf("%s: page id %v, RPC id %v", name, ssr, rpc)
		}
	}
}

// An explicit Repeater ID is reserved, so a later auto repeater on the
// page takes the next id instead of repeating it and its -items region.
func TestRepeaterReservesExplicitID(t *testing.T) {
	page := render.WithIDScope(context.Background())
	row := func(int) render.HTML { return render.Text("row") }
	first := string(Repeater(RepeaterConfig{ID: "rep", Name: "a", Label: "A", Ctx: page, Template: row}))
	second := string(Repeater(RepeaterConfig{Name: "b", Label: "B", Ctx: page, Template: row}))
	a, b := firstID.FindStringSubmatch(first), firstID.FindStringSubmatch(second)
	if a == nil || b == nil || a[1] == b[1] {
		t.Fatalf("explicit repeater id %v, auto repeater id %v; want them to differ", a, b)
	}
}

// On a page (one id region), an id a component forwards without
// reserving it still wins: FormRepeater's Name and AnimatedCounter's ID
// keep theirs and the auto Repeater and Counter move.
func TestWrittenIDsWinOnAPage(t *testing.T) {
	row := func(int) render.HTML { return render.Text("row") }
	cases := map[string]func(context.Context) (render.HTML, render.HTML){
		"form repeater": func(ctx context.Context) (render.HTML, render.HTML) {
			return FormRepeater(FormRepeaterConfig{Name: "rep", Ctx: ctx}),
				Repeater(RepeaterConfig{Name: "other", Label: "Other", Ctx: ctx, Template: row})
		},
		"animated counter": func(ctx context.Context) (render.HTML, render.HTML) {
			return AnimatedCounter(AnimatedCounterConfig{ID: "counter", To: 1, Ctx: ctx}),
				Counter(CounterConfig{SignalName: "n", Ctx: ctx})
		},
	}
	for name, render2 := range cases {
		ids := render.NewRegionBatch(render.WithIDScope(context.Background()), 1)
		explicit, auto := render2(ids.Context(0))
		page := explicit + auto
		ids.ReserveRendered(0, page)
		page = ids.Settle(0)(page)
		got := firstID.FindAllStringSubmatch(string(page), -1)
		seen := map[string]bool{}
		for _, m := range got {
			if seen[m[1]] {
				t.Errorf("%s: id %q repeats on the page", name, m[1])
			}
			seen[m[1]] = true
		}
	}
}
