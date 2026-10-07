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
// an empty state lost its bottom half on a phone.
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
	body := `<div id="empty">` + string(empty) + `</div><div id="tall">` + string(tall) + `</div>`
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
}
