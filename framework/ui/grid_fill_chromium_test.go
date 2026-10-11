package ui_test

import (
	"math"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A dashboard's card groups: an overline section heading reads as a
// small upper-case label, a Fill grid keeps a lone card at a column's
// width, and stat cards wrapped in polled regions share their row's
// height.
func TestCardGroupsKeepTheirColumns(t *testing.T) {
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
	wrap := func(c ui.StatCardConfig) render.HTML {
		return html.Div(html.DivConfig{}, ui.StatCard(c))
	}
	row := ui.Grid(ui.GridConfig{Min: "14rem", Fill: true},
		ui.StatCard(ui.StatCardConfig{Label: "Customers", Value: "11", Trend: "Updated 6h ago"}),
		ui.StatCard(ui.StatCardConfig{Label: "Payments", Value: "0"}),
	)
	wrapped := ui.Grid(ui.GridConfig{Min: "14rem"},
		wrap(ui.StatCardConfig{Label: "Customers", Value: "11", Trend: "Updated 6h ago"}),
		wrap(ui.StatCardConfig{Label: "Payments", Value: "0"}),
	)
	lone := ui.Section(ui.SectionConfig{Heading: "Catalog", Overline: true},
		ui.Grid(ui.GridConfig{Min: "14rem", Fill: true, ID: "lone"},
			ui.StatCard(ui.StatCardConfig{Label: "Plans", Value: "3"})))
	plain := ui.Section(ui.SectionConfig{Heading: "Billing"}, row)
	body := `<div style="width: 1000px; padding: 16px">` + string(plain) + `<div id="wrapped">` + string(wrapped) + `</div>` + string(lone) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1100, 900))
	var got map[string]any
	probe := `(() => {
		const heads = document.querySelectorAll(".fui-section__heading");
		const plain = getComputedStyle(heads[0]), over = getComputedStyle(heads[1]);
		const lone = document.querySelector("#lone [data-cui-comp=ui-stat-card]").getBoundingClientRect();
		const grid = document.querySelector("#lone").getBoundingClientRect();
		const w = [...document.querySelectorAll("#wrapped [data-cui-comp=ui-stat-card]")].map(e => e.getBoundingClientRect().height);
		const cell = document.querySelector("#wrapped [data-cui-comp=ui-stat-card]").parentElement.getBoundingClientRect().height;
		return {
			overCase: over.textTransform, overSize: parseFloat(over.fontSize), plainSize: parseFloat(plain.fontSize),
			overColor: over.color, plainColor: plain.color, plainCase: plain.textTransform,
			loneW: lone.width, gridW: grid.width, h0: w[0], h1: w[1], cell: cell,
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	f := func(k string) float64 { v, _ := got[k].(float64); return v }
	if got["overCase"] != "uppercase" || got["plainCase"] == "uppercase" || f("overSize") >= f("plainSize") || got["overColor"] == got["plainColor"] {
		t.Errorf("the overline heading is not a small muted upper-case label beside a plain one: %v", got)
	}
	if f("loneW") > f("gridW")/2 {
		t.Errorf("a lone card in a Fill grid stretched across the row: %v", got)
	}
	if f("h0") < 1 || math.Abs(f("h0")-f("h1")) > 0.5 || math.Abs(f("h0")-f("cell")) > 0.5 {
		t.Errorf("wrapped stat cards in one row differ in height or overflow their cell: %v", got)
	}
}
