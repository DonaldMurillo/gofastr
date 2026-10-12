package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// On a phone a compact trail in a toolbar stays on one line: the lead
// Shrink cluster keeps its menu control whole and gives the trail what
// is left, and the crumbs ellipsize instead of stacking over two lines
// ("Customers / New Customer" did in the admin). The trailing controls
// keep their place on the row.
func TestCompactTrailStaysOneLine(t *testing.T) {
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
	trail := ui.Breadcrumbs(ui.BreadcrumbsConfig{CompactMobile: true},
		ui.Crumb{Text: "Meridian", Href: "/admin"},
		ui.Crumb{Text: "Customers with a long plural name", Href: "/admin/customers"},
		ui.Crumb{Text: "New Customer With A Long Title", Current: true})
	toolbar := ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween, Align: ui.AlignCenter, NoWrap: true},
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true, Shrink: true},
			render.HTML(`<span id="menu">Open menu</span>`),
			render.HTML(`<div id="area">`)+trail+render.HTML(`</div>`)),
		render.HTML(`<button id="end" type="button">Search and more</button>`))
	body := `<div style="padding: 16px">` + string(toolbar) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(390, 700))
	var got map[string]float64
	probe := `(() => {
		const r = s => document.querySelector(s).getBoundingClientRect();
		return {
			trailH: r("[data-cui-comp=ui-breadcrumbs]").height,
			lineH: parseFloat(getComputedStyle(document.querySelector(".fui-breadcrumbs__list")).fontSize) * 2,
			menuH: r("#menu").height,
			endRight: r("#end").right, W: document.documentElement.clientWidth,
			pageScroll: document.documentElement.scrollWidth,
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["trailH"] >= got["lineH"] {
		t.Errorf("the trail wraps: %vpx tall at a %vpx line pair", got["trailH"], got["lineH"])
	}
	if got["menuH"] >= got["lineH"] {
		t.Errorf("the lead control was squeezed onto two lines: %vpx tall", got["menuH"])
	}
	if got["endRight"] > got["W"] || got["pageScroll"] > got["W"] {
		t.Errorf("the row overflows the phone: %v", got)
	}
}
