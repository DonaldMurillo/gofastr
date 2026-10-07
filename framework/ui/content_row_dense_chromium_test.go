package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// A Dense row tightens its controls and table rows to the compact
// height on a mouse, and a touch screen keeps the 44px targets. A
// table under a compact theme scope follows the density height too.
func TestContentRowDenseOnFinePointer(t *testing.T) {
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
	frame := func(id string, dense bool) string {
		tbl := ui.DataTable(ui.DataTableConfig{
			Path:    "/x",
			Columns: []ui.Column{{Key: "pick", SelectAll: "ids", Fit: true}, {Key: "name", Header: "Name", Sortable: true}},
			Rows: []ui.Row{{ID: "r1", Cells: map[string]render.HTML{
				"pick": ui.Checkbox(ui.ToggleConfig{Name: "ids", Value: "1", ID: "sel-" + id, Label: "Select", LabelHidden: true}),
				"name": render.Text("INV-1010"),
			}}},
		})
		btn := ui.Button(ui.ButtonConfig{Label: "New"})
		search := ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "q-" + id})
		return `<div id="` + id + `">` + string(ui.ContentRow(ui.ContentRowConfig{Dense: dense}, `<main><p>`+btn+`</p>`+search+tbl+`</main>`)) + `</div>`
	}
	// The compact frame stands in for a theme boundary with the compact
	// density: it declares the option value and leaves the touch target.
	compact := `<div id="compact" style="--fui-density-control-h: 36px">` + string(ui.DataTable(ui.DataTableConfig{
		Path:    "/x",
		Columns: []ui.Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []ui.Row{{ID: "r1", Cells: map[string]render.HTML{"name": render.Text("INV-1010")}}},
	})) + `</div>`
	srv := themeTestPageWithHead(t, head, frame("dense", true)+frame("plain", false)+compact)
	probe := `(() => {
		// Collapsed borders split the header's rule across rows, so a
		// height can carry half a pixel.
		const h = (id, sel) => Math.floor(document.querySelector("#" + id + " " + sel).getBoundingClientRect().height);
		const out = {fine: matchMedia("(pointer: fine)").matches};
		for (const id of ["dense", "plain"]) {
			out[id] = {row: h(id, "tbody td"), head: h(id, "thead th"), button: h(id, ".fui-button"), search: h(id, ".fui-search")};
		}
		out.compact = {row: h("compact", "tbody td"), head: h("compact", "thead th")};
		return out;
	})()`
	type sizes struct{ Row, Head, Button, Search float64 }
	var got struct {
		Fine                  bool
		Dense, Plain, Compact sizes
	}

	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1000, 600))
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !got.Fine {
		t.Fatalf("the desktop context reports no fine pointer, so the check proves nothing: %+v", got)
	}
	if got.Dense != (sizes{Row: 44, Head: 36, Button: 36, Search: 36}) {
		t.Errorf("a dense row on a mouse = %+v, want rows 44 and 36px controls", got.Dense)
	}
	if got.Plain != (sizes{Row: 52, Head: 44, Button: 44, Search: 44}) {
		t.Errorf("a plain row = %+v, want rows 52 and 44px controls", got.Plain)
	}
	if got.Compact.Row != 44 || got.Compact.Head != 36 {
		t.Errorf("a table under the compact density = %+v, want rows 44 and a 36px header", got.Compact)
	}

	touch := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(390, 800),
		emulation.SetTouchEmulationEnabled(true).WithMaxTouchPoints(5))
	if err := chromedp.Run(touch, chromedp.Reload(), chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("touch probe: %v", err)
	}
	if got.Fine {
		t.Fatalf("touch emulation still reports a fine pointer: %+v", got)
	}
	if got.Dense != (sizes{Row: 52, Head: 44, Button: 44, Search: 44}) {
		t.Errorf("a dense row on a touch screen = %+v, want the 44px targets", got.Dense)
	}
}
