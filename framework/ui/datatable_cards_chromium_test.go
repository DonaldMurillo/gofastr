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

// In cards mode a cell grows to its content. The table's 52px row
// rhythm is a minimum inside table layout but an exact height once a
// cell is a flex box, and the scroll wrapper clipped what overflowed:
// an empty state lost its bottom half on a phone. A value made of
// several parts ("Role · billing") reads as one run at the line's end,
// where each part had been its own flex item, spread across the card;
// a label-less cell's controls keep their gap.
func TestDataTableCardsCellsFitContent(t *testing.T) {
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
	cols := []ui.Column{{Key: "a", Header: "Invoice"}, {Key: "b", Header: "Amount"}}
	empty := ui.DataTable(ui.DataTableConfig{
		Columns: cols, Responsive: ui.ResponsiveCards,
		Empty: ui.EmptyStateConfig{Title: "No payments yet", Description: "They will appear here once created."},
	})
	tall := ui.DataTable(ui.DataTableConfig{
		Columns: cols, Responsive: ui.ResponsiveCards,
		Rows: []ui.Row{{Cells: map[string]render.HTML{
			"a": render.Text(strings.Repeat("a long wrapping value ", 8)),
			"b": render.Text("1.00"),
		}}},
	})
	mixed := ui.DataTable(ui.DataTableConfig{
		Columns:    []ui.Column{{Key: "a", Header: "Record"}, {Key: "b"}},
		Responsive: ui.ResponsiveCards,
		Rows: []ui.Row{{Cells: map[string]render.HTML{
			"a": render.HTML(`<span id="p1">Role · </span><b id="p2">billing</b>`),
			"b": render.HTML(`<a id="c1" href="/a">Edit</a><a id="c2" href="/b">View</a>`),
		}}},
	})
	body := `<div id="empty">` + string(empty) + `</div><div id="tall">` + string(tall) + `</div><div id="mixed">` + string(mixed) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(390, 844), prefersScheme("light"))

	var png []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&png, 100)); err == nil {
		_ = os.WriteFile(filepath.Join(t.ArtifactDir(), "cards.png"), png, 0o600)
	}
	var got map[string]any
	geom := `(() => {
		const out = {};
		for (const id of ["empty", "tall"]) {
			const sc = document.querySelector("#" + id + " .fui-data-table__scroll");
			const thead = document.querySelector("#" + id + " thead");
			out[id + "Cards"] = getComputedStyle(thead).position === "absolute";
			out[id + "Clipped"] = sc.scrollHeight - sc.clientHeight;
			out[id + "Wide"] = sc.scrollWidth - sc.clientWidth;
		}
		const td = document.querySelector("#empty tbody td");
		const es = document.querySelector("#empty .fui-empty-state");
		const pad = parseFloat(getComputedStyle(td).paddingInlineStart) + parseFloat(getComputedStyle(td).paddingInlineEnd);
		out.emptyGap = td.clientWidth - pad - es.getBoundingClientRect().width;
		const r = id => document.getElementById(id).getBoundingClientRect();
		out.partGap = r("p2").left - r("p1").right;
		out.partEnd = r("p2").right;
		out.cellEnd = document.querySelector("#mixed tbody td").getBoundingClientRect().right;
		out.controlGap = r("c2").left - r("c1").right;
		return out;
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(geom, &got)); err != nil {
		t.Fatalf("geometry: %v", err)
	}
	for _, id := range []string{"empty", "tall"} {
		if got[id+"Cards"] != true {
			t.Fatalf("%s table is not in cards mode at 390px: %v", id, got)
		}
		if px, _ := got[id+"Clipped"].(float64); px > 1 {
			t.Errorf("%s table clips %vpx of its cards", id, px)
		}
		// A card's long value wraps inside the card; it does not keep
		// the table's one-line cells and scroll sideways.
		if px, _ := got[id+"Wide"].(float64); px > 1 {
			t.Errorf("%s table's cards run %vpx past the phone", id, px)
		}
	}
	// The empty state fills its card, not the end half a label-less
	// actions cell aligns to.
	if px, _ := got["emptyGap"].(float64); px > 1 {
		t.Errorf("empty state is %vpx narrower than its card", px)
	}
	if px, _ := got["partGap"].(float64); px > 1 {
		t.Errorf("a value's parts sit %vpx apart; they read as one run", px)
	}
	if end, cell := got["partEnd"].(float64), got["cellEnd"].(float64); cell-end > 1 {
		t.Errorf("a value ends %vpx short of its card line's end", cell-end)
	}
	if px, _ := got["controlGap"].(float64); px < 4 {
		t.Errorf("a label-less cell's controls sit %vpx apart; they need their gap", px)
	}
}
