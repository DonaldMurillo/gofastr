//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: a Hero's Actions row pairs a filled CTA with a quieter
// secondary affordance (ui.LinkButton + ui.Link). At --spacing-sm the
// two boxes sat 4px apart, which reads as touching at desktop widths
// (caught in the second layout eval at 1280px on a split hero). The
// row's gap is the hero's own styling surface, so the sheet owns the
// fix: --spacing-md between sibling actions, in both hero variants
// (the one rule selects .fui-hero__actions under the single-column and
// the --split root alike). This renders real heroes in Chrome and
// measures the painted distance between the two action boxes.
func TestHeroActionsGapSeparatesActions(t *testing.T) {
	actions := func() []render.HTML {
		return []render.HTML{
			LinkButton(LinkButtonConfig{Label: "Start", Href: "/start"}),
			Link(LinkConfig{Text: "View documentation", Href: "/docs"}),
		}
	}
	page := render.HTML(string(Hero(HeroConfig{
		ExtraAttrs: html.Attrs{"data-hero": "plain"},
		Title:      "Plain",
		Actions:    actions(),
	})) + string(Hero(HeroConfig{
		ExtraAttrs: html.Attrs{"data-hero": "split"},
		Title:      "Split",
		Subtitle:   "Copy beside media.",
		Media:      html.Div(html.DivConfig{}, render.Text("media")),
		Actions:    actions(),
	})))
	css := heroStyle.Entry().CSSFor(theme.Default()) +
		buttonStyle.Entry().CSSFor(theme.Default()) +
		linkStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0}</style>
<style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1280, 800))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()
	var m map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const measure = hero => {
				const row = document.querySelector('[data-hero="' + hero + '"] .fui-hero__actions');
				const kids = [...row.children];
				const a = kids[0].getBoundingClientRect();
				const b = kids[1].getBoundingClientRect();
				return {boxGap: b.left - a.right,
					rowGap: parseFloat(getComputedStyle(row).gap) || 0,
					sameRow: Math.abs(a.top - b.top)};
			};
			return {...measure('plain'), splitBoxGap: measure('split').boxGap,
				splitRowGap: measure('split').rowGap, splitSameRow: measure('split').sameRow};
		})()`, &m),
	); err != nil {
		t.Fatal(err)
	}
	// Anti-vacuity: both actions must actually share one row in each
	// variant, or the horizontal distance measures nothing.
	for _, c := range []struct {
		name            string
		sameRow, boxGap float64
	}{{"plain", m["sameRow"], m["boxGap"]}, {"split", m["splitSameRow"], m["splitBoxGap"]}} {
		if c.sameRow > 1 {
			t.Fatalf("%s hero: actions wrapped onto separate rows (top delta %.1fpx); widen the viewport", c.name, c.sameRow)
		}
		// --spacing-md is 8px in the default theme; the sheet's fallback
		// restates it (fallbacks restate their token).
		if c.boxGap+0.5 < 8 {
			t.Errorf("%s hero: actions sit %.1fpx apart, want at least the --spacing-md token (8px)", c.name, c.boxGap)
		}
	}
}
