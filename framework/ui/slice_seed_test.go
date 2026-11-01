package ui_test

// The components that render their own binding over a store.Slice
// stamp the slice's value for THIS request (what a producer Seeded),
// not its declared default: the server's first paint, the page's seed
// and the runtime's first step must all name one value, and a reader
// without script sees the seeded one.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var (
	sliceSeedQty    = store.New("sliceseed").Int("qty", 0)
	sliceSeedTab    = store.New("sliceseed").Int("tab", 0)
	sliceSeedToggle = store.New("sliceseed").Bool("on", false)
)

// counterValueRe captures the counter's value span text, the element
// carrying the signal binding (data-cui-signal="sliceseed.qty").
var counterValueRe = regexp.MustCompile(`data-cui-signal="sliceseed\.qty"[^>]*>([^<]*)<`)

func TestCounterSliceStampsTheRequestSeed(t *testing.T) {
	ctx := store.WithValues(context.Background())
	sliceSeedQty.Seed(ctx, 7)
	out := string(render.SettleIDs(ctx, string(ui.Counter(ui.CounterConfig{Slice: sliceSeedQty, Ctx: ctx}))))
	m := counterValueRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no counter value span bound to sliceseed.qty: %s", out)
	}
	if m[1] != "7" {
		t.Fatalf("a request that seeds 7 renders the counter as %q, want 7: first paint shows the slice default while + steps from the seed\n%s", m[1], out)
	}
	if got := store.ResolveSeed(ctx, []string{sliceSeedQty.Name()})[sliceSeedQty.Name()]; got != 7 {
		t.Fatalf("the page seed carries %v, want 7", got)
	}
}

// Without a seed (or without a value bag) the default still renders.
func TestCounterSliceWithoutSeedStampsTheDefault(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"no ctx":       nil,
		"no value bag": context.Background(),
		"unseeded bag": store.WithValues(context.Background()),
	} {
		out := string(ui.Counter(ui.CounterConfig{Slice: sliceSeedQty, Ctx: ctx}))
		m := counterValueRe.FindStringSubmatch(out)
		if m == nil || m[1] != "0" {
			t.Fatalf("%s: counter text = %v, want the default 0\n%s", name, m, out)
		}
	}
}

func TestTabsSliceSelectsTheRequestSeed(t *testing.T) {
	ctx := store.WithValues(context.Background())
	sliceSeedTab.Seed(ctx, 2)
	out := string(ui.Tabs(ui.TabsConfig{Slice: sliceSeedTab, Ctx: ctx, Tabs: []ui.TabItem{
		{Label: "One", Content: "1"}, {Label: "Two", Content: "2"}, {Label: "Three", Content: "3"},
	}}))
	if !strings.Contains(out, `data-active="2"`) {
		t.Fatalf("a request that seeds tab 2 renders a strip without data-active=\"2\": the slice default won the first paint\n%s", out)
	}
	sel := regexp.MustCompile(`aria-selected="true"[^>]*data-cui-tab-index="(\d+)"|data-cui-tab-index="(\d+)"[^>]*aria-selected="true"`).FindStringSubmatch(out)
	if sel == nil || (sel[1] != "2" && sel[2] != "2") {
		t.Fatalf("the selected tab is %v, want index 2\n%s", sel, out)
	}
}

// A seed past the last tab paints tab 0 (headless: out of range takes
// 0) while the page seed still carries the request's 5; the
// headless-tabs module writes 0 back into the signal on wire
// (framework/headless TestE2E_TabsOutOfRangeSeedSettlesToPaintedTab), so
// the paint is the value the runtime ends up holding.
func TestTabsSliceOutOfRangeSeedPaintsTabZero(t *testing.T) {
	for _, seed := range []int{5, 3, -1} {
		ctx := store.WithValues(context.Background())
		sliceSeedTab.Seed(ctx, seed)
		out := string(ui.Tabs(ui.TabsConfig{Slice: sliceSeedTab, Ctx: ctx, Tabs: []ui.TabItem{
			{Label: "One", Content: "1"}, {Label: "Two", Content: "2"}, {Label: "Three", Content: "3"},
		}}))
		if !strings.Contains(out, `data-active="0"`) || strings.Count(out, `aria-selected="true"`) != 1 {
			t.Fatalf("seed %d: the strip does not paint tab 0 alone\n%s", seed, out)
		}
		if !regexp.MustCompile(`aria-selected="true"[^>]*data-cui-tab-index="0"`).MatchString(out) {
			t.Fatalf("seed %d: the selected tab is not index 0\n%s", seed, out)
		}
	}
}

func TestSignalToggleSliceStampsTheRequestSeed(t *testing.T) {
	ctx := store.WithValues(context.Background())
	sliceSeedToggle.Seed(ctx, true)
	out := string(ui.SignalToggle(ui.SignalToggleConfig{Slice: sliceSeedToggle, Ctx: ctx}))
	if !strings.Contains(out, `aria-checked="true"`) {
		t.Fatalf("a request that seeds true renders the switch unchecked: the slice default won the first paint\n%s", out)
	}
	if !strings.Contains(out, `data-cui-signal="sliceseed.on">true<`) {
		t.Fatalf("the toggle's label does not show the seeded true\n%s", out)
	}
	off := string(ui.SignalToggle(ui.SignalToggleConfig{Slice: sliceSeedToggle}))
	if !strings.Contains(off, `aria-checked="false"`) {
		t.Fatalf("with no Ctx the toggle should stamp the default false\n%s", off)
	}
}
