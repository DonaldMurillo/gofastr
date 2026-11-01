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
