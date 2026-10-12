//go:build chromium

package ui

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: the featured plan's "Recommended" badge sat on its own
// line above the plan name, so the featured card's price started 27px
// lower than its neighbours' and the row of prices read as ragged
// (caught screenshotting a three-plan pricing band). The badge rides
// the name's line instead; this renders a plain and a featured card side
// by side and compares where their names and prices paint.
func TestPricingBadgeKeepsPricesAligned(t *testing.T) {
	card := func(id string, featured bool) render.HTML {
		return PricingCard(PricingCardConfig{
			Name: "Team", Description: "For teams shipping fast.", Price: "$99", Period: "/mo",
			Features: []string{"One", "Two"}, CTALabel: "Choose", CTAHref: "/x",
			Featured: featured, ExtraAttrs: html.Attrs{"data-card": id},
		})
	}
	page := Grid(GridConfig{Min: "16rem"}, card("plain", false), card("featured", true))
	css := layoutStyle.Entry().CSSFor(theme.Default()) +
		pricingCardStyle.Entry().CSSFor(theme.Default()) +
		buttonStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;width:800px}</style>
<style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1000, 800))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const top = (card, sel) => document.querySelector('[data-card="' + card + '"] ' + sel).getBoundingClientRect().top;
			const badge = document.querySelector('[data-card="featured"] .fui-pricing-card__badge').getBoundingClientRect();
			const name = document.querySelector('[data-card="featured"] .fui-pricing-card__name').getBoundingClientRect();
			return {plainCard: top('plain', ''), featuredCard: top('featured', ''),
				plainPrice: top('plain', '.fui-pricing-card__amount'), featuredPrice: top('featured', '.fui-pricing-card__amount'),
				plainName: top('plain', '.fui-pricing-card__name'), featuredName: name.top,
				badgeLeft: badge.left, nameRight: name.right, badgeW: badge.width};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	// Anti-vacuity: the two cards share a row and the badge painted.
	if math.Abs(m["plainCard"]-m["featuredCard"]) > 1 || m["badgeW"] == 0 {
		t.Fatalf("cards not side by side or badge not painted: %v", m)
	}
	if d := m["featuredPrice"] - m["plainPrice"]; math.Abs(d) > 1 {
		t.Errorf("featured price paints %.1fpx below its neighbour's; the badge must not push the card's content down", d)
	}
	if d := m["featuredName"] - m["plainName"]; math.Abs(d) > 1 {
		t.Errorf("featured name paints %.1fpx off its neighbour's", d)
	}
	if m["badgeLeft"] < m["nameRight"] {
		t.Errorf("badge (left %.1f) overlaps the plan name (right %.1f)", m["badgeLeft"], m["nameRight"])
	}
}

// Hard rule 9: a plan whose description wrapped to two lines painted its
// price a line below its neighbours' (caught on a generated /pricing).
// Cards in one grid row share their row tracks, so the head, price,
// features and button each start on one line across the row.
func TestPricingRowsAlignAcrossCards(t *testing.T) {
	card := func(id, desc string, features ...string) render.HTML {
		return PricingCard(PricingCardConfig{
			Name: "Plan", Description: desc, Price: "$29", Period: "/mo",
			Features: features, CTALabel: "Choose", CTAHref: "/x",
			ExtraAttrs: html.Attrs{"data-card": id},
		})
	}
	page := Grid(GridConfig{Min: "16rem"},
		card("short", "Short.", "One"),
		card("long", "A description long enough to wrap onto a second and maybe a third line in its column.", "One", "Two", "Three"))
	css := layoutStyle.Entry().CSSFor(theme.Default()) +
		pricingCardStyle.Entry().CSSFor(theme.Default()) +
		buttonStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const r = (card, sel) => document.querySelector('[data-card="' + card + '"] ' + sel).getBoundingClientRect();
		return {cardS: r('short', '').top, cardL: r('long', '').top,
			descS: r('short', '.fui-pricing-card__desc').height, descL: r('long', '.fui-pricing-card__desc').height,
			priceS: r('short', '.fui-pricing-card__amount').top, priceL: r('long', '.fui-pricing-card__amount').top,
			ctaS: r('short', '.fui-pricing-card__cta').top, ctaL: r('long', '.fui-pricing-card__cta').top,
			featS: r('short', '.fui-pricing-card__features').top, featL: r('long', '.fui-pricing-card__features').top};
	})()`)
	if math.Abs(m["cardS"]-m["cardL"]) > 1 || m["descL"] <= m["descS"] {
		t.Fatalf("cards not side by side or the long description did not wrap: %v", m)
	}
	for _, part := range []string{"price", "feat", "cta"} {
		if d := m[part+"L"] - m[part+"S"]; math.Abs(d) > 1 {
			t.Errorf("%s paints %.0fpx off between the cards; a row of plans shares its lines", part, d)
		}
	}
}

// Hard rule 9: the shared row lines are for a grid of plans only. A card
// wrapped in a cell of its own left its button stranded mid-card, and a
// plain card beside two plans was squeezed into the plans' first row
// (caught in review). A wrapped card keeps its button at the bottom and
// a mixed grid stretches every card.
func TestPricingOutsideAPlanGrid(t *testing.T) {
	card := func(id string, features ...string) render.HTML {
		return PricingCard(PricingCardConfig{
			Name: "Plan", Price: "$29", Features: features, CTALabel: "Choose", CTAHref: "/x",
			ExtraAttrs: html.Attrs{"data-card": id},
		})
	}
	page := Grid(GridConfig{Min: "12rem"},
		render.HTML(`<div>`)+card("wrapShort", "One")+`</div>`,
		render.HTML(`<div>`)+card("wrapLong", "One", "Two", "Three", "Four", "Five")+`</div>`) +
		Grid(GridConfig{Min: "12rem"},
			card("mixPlan", "One", "Two", "Three", "Four", "Five"),
			Card(CardConfig{ExtraAttrs: html.Attrs{"data-card": "mixPlain"}}, render.HTML("<p>Plain</p>")))
	css := layoutStyle.Entry().CSSFor(theme.Default()) +
		pricingCardStyle.Entry().CSSFor(theme.Default()) +
		buttonStyle.Entry().CSSFor(theme.Default()) +
		cardStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const r = (card, sel) => document.querySelector('[data-card="' + card + '"] ' + sel).getBoundingClientRect();
		return {wrapCardS: r('wrapShort', '').bottom, wrapCtaS: r('wrapShort', '.fui-pricing-card__cta').bottom,
			wrapCardL: r('wrapLong', '').bottom, plainH: r('mixPlain', '').height, planH: r('mixPlan', '').height};
	})()`)
	if m["wrapCardL"] == 0 || m["planH"] == 0 {
		t.Fatalf("cards not laid out: %v", m)
	}
	if gap := m["wrapCardS"] - m["wrapCtaS"]; gap > 40 {
		t.Errorf("a wrapped card's button ends %.0fpx above its bottom edge; it belongs at the bottom", gap)
	}
	if d := m["planH"] - m["plainH"]; d > 1 {
		t.Errorf("a plain card beside a plan is %.0fpx shorter; a mixed grid stretches every card", d)
	}
}
