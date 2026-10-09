package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// On a phone a rows-mode table is a list of two-line rows: the title
// over the subtitle, the meta over the detail at the end, the selection
// box before and the row menu after. Unslotted columns are not drawn
// and nothing scrolls sideways. On a wide screen it stays a table.
func TestDataTableRowsOnPhone(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := `<meta name="viewport" content="width=device-width, initial-scale=1">` +
		"<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	table := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{
			{Key: "s", Fit: true, Phone: ui.PhoneLead},
			{Key: "name", Header: "Name", Phone: ui.PhoneTitle},
			{Key: "email", Header: "Email", Phone: ui.PhoneSubtitle},
			{Key: "company", Header: "Company"},
			{Key: "notes", Header: "Notes", Wrap: true},
			{Key: "status", Header: "Status", Phone: ui.PhoneMeta},
			{Key: "mrr", Header: "MRR", Align: "end", Phone: ui.PhoneDetail},
			{Key: "a", Fit: true, Align: "end", Phone: ui.PhoneEnd},
		},
		Responsive: ui.ResponsiveRows,
		Rows: []ui.Row{{Cells: map[string]render.HTML{
			"s":       ui.Checkbox(ui.ToggleConfig{Name: "ids", ID: "sel", Value: "1", Label: "Select Ada", LabelHidden: true}),
			"name":    render.HTML(`<a id="title" href="/c/1">Ada Lovelace of the Analytical Engine Company</a>`),
			"email":   render.HTML(`<span id="sub">ada@analytical-engine.example</span>`),
			"company": render.HTML(`<span id="co">Babbage Ltd</span>`),
			"notes":   render.Text(strings.Repeat("a long note ", 20)),
			"status":  render.HTML(`<span id="meta">active</span>`),
			"mrr":     render.HTML(`<span id="detail">$99.00</span>`),
			"a":       render.HTML(`<button id="menu">…</button>`),
		}}},
	})
	body := `<div id="t">` + string(table) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	geom := `(() => {
		const r = id => { const e = document.getElementById(id); return (e.closest("td") || e).getBoundingClientRect(); };
		const sc = document.querySelector("#t .fui-data-table__scroll");
		return {
			wide: sc.scrollWidth - sc.clientWidth,
			coShown: r("co").width > 0,
			titleBottom: r("title").bottom, subTop: r("sub").top,
			titleLeft: r("title").left, subLeft: r("sub").left,
			selRight: r("sel").right, metaLeft: r("meta").left,
			selWidth: document.getElementById("sel").getBoundingClientRect().width,
			selClipped: (() => { const b = document.getElementById("sel").getBoundingClientRect(), c = document.getElementById("sel").closest("td").getBoundingClientRect(); return b.left < c.left - 0.5 || b.right > c.right + 0.5; })(),
			titleRight: r("title").right, metaBottom: r("meta").bottom,
			detailTop: r("detail").top, menuLeft: r("menu").left,
			detailRight: r("detail").right,
			theadHidden: getComputedStyle(document.querySelector("#t thead")).position === "absolute",
		};
	})()`

	phone := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(375, 812), prefersScheme("light"))
	var png []byte
	if err := chromedp.Run(phone, chromedp.FullScreenshot(&png, 100)); err == nil {
		_ = os.WriteFile(filepath.Join(t.ArtifactDir(), "rows.png"), png, 0o600)
	}
	var got map[string]any
	if err := chromedp.Run(phone, chromedp.Evaluate(geom, &got)); err != nil {
		t.Fatalf("geometry: %v", err)
	}
	f := func(k string) float64 { v, _ := got[k].(float64); return v }
	if got["theadHidden"] != true {
		t.Fatalf("the header row shows on a phone: %v", got)
	}
	if f("wide") > 1 {
		t.Errorf("the phone rows scroll %vpx sideways", f("wide"))
	}
	if got["coShown"] == true {
		t.Errorf("an unslotted column is drawn on a phone")
	}
	if f("subTop") < f("titleBottom")-1 || f("subLeft")-f("titleLeft") > 1 || f("titleLeft")-f("subLeft") > 1 {
		t.Errorf("the subtitle is not under the title: %v", got)
	}
	if f("selWidth") < 12 || got["selClipped"] == true {
		t.Errorf("the selection box is clipped on a phone: %v", got)
	}
	if f("titleLeft") < f("selRight") {
		t.Errorf("the selection box is not before the title: %v", got)
	}
	if f("metaLeft") < f("titleRight") {
		t.Errorf("the meta is not after the title: %v", got)
	}
	if f("detailTop") < f("metaBottom")-1 {
		t.Errorf("the detail is not under the meta: %v", got)
	}
	if f("menuLeft") < f("detailRight") {
		t.Errorf("the row menu is not at the end: %v", got)
	}

	desk := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1280, 800), prefersScheme("light"))
	if err := chromedp.Run(desk, chromedp.Evaluate(geom, &got)); err != nil {
		t.Fatalf("geometry: %v", err)
	}
	if got["theadHidden"] == true || got["coShown"] != true {
		t.Errorf("a wide rows-mode table is not a table: %v", got)
	}
}

// A truncated column holds its value to one capped line with an
// ellipsis; the table does not widen past its box for it.
func TestDataTableTruncatedColumn(t *testing.T) {
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
	table := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "a", Header: "Job"}, {Key: "e", Header: "Last error", Truncate: true}},
		Rows: []ui.Row{{Cells: map[string]render.HTML{
			"a": render.Text("j1"),
			"e": ui.InlineCodeDanger("webhook: 502 Bad Gateway from " + strings.Repeat("https://hooks.example.com/billing/", 6)),
		}}},
	})
	srv := themeTestPageWithHead(t, head, `<div id="t" style="inline-size: 900px">`+string(table)+`</div>`)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1280, 800), prefersScheme("light"))
	var got map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const td = document.querySelector("#t td.is-truncate");
		const sc = document.querySelector("#t .fui-data-table__scroll");
		return {w: td.getBoundingClientRect().width, cut: td.scrollWidth > td.clientWidth, h: td.getBoundingClientRect().height, wide: sc.scrollWidth - sc.clientWidth};
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	if got["cut"] != true {
		t.Errorf("the value was not cut: %v", got)
	}
	if wide, _ := got["wide"].(float64); wide > 1 {
		t.Errorf("the table scrolls %vpx sideways", wide)
	}
}
