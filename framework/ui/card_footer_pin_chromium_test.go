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

// Hard rule 9: footer placement is layout, invisible to markup dumps. A
// config-only card (Heading/Description/Footer, no body children) has no
// flex-stretch element inside, so in any stretch context — a Grid row, an
// align-stretch flex parent — its footer used to float mid-card with dead
// space beneath it (caught screenshotting the relayboard recipe's pricing
// grid). The footer's margin-top:auto is the guard; this renders a real
// Grid in Chrome and measures it.
func TestCardFooterPinsToBottomInGrid(t *testing.T) {
	page := Grid(GridConfig{Min: "12rem"},
		Card(CardConfig{Heading: "Tall", Footer: render.Text("footer")},
			html.Paragraph(html.TextConfig{}, render.Text(
				"Long body copy that wraps onto several lines so this card sets "+
					"the grid row height and the sibling cards must stretch to match it, "+
					"padding padding padding padding padding padding padding."))),
		Card(CardConfig{Heading: "Short", Description: "Config-only card.",
			Footer: render.Text("footer")}),
		// The linked variant nests everything in .ui-card__inner; unless
		// that wrapper grows to fill the stretched <a>, the footer pins
		// to the inner's edge, not the card's.
		Card(CardConfig{Heading: "Linked", Description: "Config-only linked card.",
			Href: "/somewhere", Footer: render.Text("footer")}),
	)
	css := layoutStyle.Entry().CSSFor(theme.Default()) + cardStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0}
%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	// Viewport width is browser setup, not page styling: 800px makes the
	// 12rem-min grid pack all three cards into one row deterministically.
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.NoSandbox, chromedp.WindowSize(800, 600))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	// For each card: its top edge, and the gap between the card's bottom
	// edge and its footer's bottom edge. A pinned footer sits on the edge
	// (≈0); the regression showed up as the short card's footer floating
	// far above.
	var cards []map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-fui-comp="ui-card"]')).map(c => {
			const f = c.querySelector('.ui-card__footer');
			return {top: c.getBoundingClientRect().top,
				gap: c.getBoundingClientRect().bottom - f.getBoundingClientRect().bottom};
		})`, &cards),
	); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("expected 3 cards, measured %d", len(cards))
	}
	// Anti-vacuity: the assertion only exercises stretch while all three
	// cards share one grid row. A wrapped card is alone in its row, never
	// stretches, and would pass the gap check without testing anything.
	for i, c := range cards[1:] {
		if math.Abs(c["top"]-cards[0]["top"]) > 1 {
			t.Fatalf("card %d wrapped to another row (top %.1f vs %.1f); widen the viewport so stretch is actually exercised", i+1, c["top"], cards[0]["top"])
		}
	}
	for i, c := range cards {
		if math.Abs(c["gap"]) > 1 {
			t.Errorf("card %d: footer bottom is %.1fpx above the card bottom; footers must pin to the card edge", i, c["gap"])
		}
	}
}
