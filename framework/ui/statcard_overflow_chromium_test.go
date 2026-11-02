package ui_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// A figure has no break opportunity ("$1,000,000,001,193.00" is one
// word), so at a phone's width a large value ran out of its card: on
// Meridian's /admin/revenue at 320px it widened the page into a
// horizontal scroll, and in /admin's StatStrip it painted over the
// neighbouring cell's label. The value wraps inside its card instead.
func TestStatCardLongValueStaysInCard(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	const big = "$1,000,000,001,193.00"
	card := ui.StatCard(ui.StatCardConfig{Label: "MRR", Value: big, ID: "solo"})
	strip := ui.StatStrip(ui.StatStripConfig{Label: "Key figures", Cells: []render.HTML{
		ui.StatCard(ui.StatCardConfig{Label: "MRR", Value: big, Plain: true, ID: "cell"}),
		ui.StatCard(ui.StatCardConfig{Label: "Active customers", Value: "7", Plain: true}),
	}})
	body := `<div style="padding: 0 24px">` + string(card) + string(strip) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(320, 640))
	var got map[string]float64
	probe := `(() => {
		const r = s => document.querySelector(s).getBoundingClientRect();
		// The text's own box: the <p> keeps its cell's width while the
		// glyphs overflow it.
		const text = s => { const g = document.createRange(); g.selectNodeContents(document.querySelector(s)); return g.getBoundingClientRect(); };
		const sv = text("#solo .fui-stat-card__value"), sc = r("#solo");
		const cv = text("#cell .fui-stat-card__value"), cc = r("#cell");
		return {soloValueRight: sv.right, soloCardRight: sc.right, cellValueRight: cv.right, cellRight: cc.right,
			pageWidth: document.documentElement.scrollWidth, viewport: document.documentElement.clientWidth};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["soloValueRight"] > got["soloCardRight"] {
		t.Errorf("the value runs out of its card: %v", got)
	}
	if got["cellValueRight"] > got["cellRight"] {
		t.Errorf("the strip cell's value runs into its neighbour: %v", got)
	}
	if got["pageWidth"] > got["viewport"] {
		t.Errorf("the value widens the page into a horizontal scroll: %v", got)
	}
}
