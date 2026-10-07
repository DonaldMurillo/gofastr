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
	// page is a content row and, outside it, a button and a table where
	// a drawer would mount, plus a frame standing in for a theme boundary
	// with the compact density: it declares the option value and leaves
	// the touch target.
	page := func(dense bool) string {
		tbl := ui.DataTable(ui.DataTableConfig{
			Path:    "/x",
			Columns: []ui.Column{{Key: "pick", SelectAll: "ids", Fit: true}, {Key: "name", Header: "Name", Sortable: true}},
			Rows: []ui.Row{{ID: "r1", Cells: map[string]render.HTML{
				"pick": ui.Checkbox(ui.ToggleConfig{Name: "ids", Value: "1", ID: "sel", Label: "Select", LabelHidden: true}),
				"name": render.Text("INV-1010"),
			}}},
		})
		btn := ui.Button(ui.ButtonConfig{Label: "New"})
		search := ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "q"})
		row := ui.ContentRow(ui.ContentRowConfig{Dense: dense}, `<main><p>`+btn+`</p>`+search+tbl+`</main>`)
		overlay := `<div id="overlay">` + string(ui.Button(ui.ButtonConfig{Label: "Save"})) + `</div>`
		compact := `<div id="compact" style="--fui-density-control-h: 36px">` + string(ui.DataTable(ui.DataTableConfig{
			Path:    "/x",
			Columns: []ui.Column{{Key: "name", Header: "Name", Sortable: true}},
			Rows:    []ui.Row{{ID: "r1", Cells: map[string]render.HTML{"name": render.Text("INV-1010")}}},
		})) + `</div>`
		return `<div id="row">` + string(row) + `</div>` + overlay + compact
	}
	dense := themeTestPageWithHead(t, head, page(true))
	plain := themeTestPageWithHead(t, head, page(false))
	probe := `(() => {
		// Collapsed borders split the header's rule across rows, so a
		// height can carry half a pixel.
		const h = (sel) => Math.floor(document.querySelector(sel).getBoundingClientRect().height);
		return {
			fine: matchMedia("(pointer: fine)").matches,
			row: h("#row tbody td"), head: h("#row thead th"), button: h("#row .fui-button"),
			search: h("#row .fui-search"), overlay: h("#overlay .fui-button"),
			compactRow: h("#compact tbody td"), compactHead: h("#compact thead th"),
		};
	})()`
	type sizes struct {
		Fine                               bool
		Row, Head, Button, Search, Overlay float64
		CompactRow, CompactHead            float64
	}
	measure := func(url string, opts ...chromedp.Action) sizes {
		t.Helper()
		var got sizes
		ctx := moduleTestCtxURL(t, url, opts...)
		if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
			t.Fatalf("probe: %v", err)
		}
		return got
	}
	mouse := chromedp.EmulateViewport(1000, 600)

	got := measure(dense.URL, mouse)
	if !got.Fine {
		t.Fatalf("the desktop context reports no fine pointer, so the check proves nothing: %+v", got)
	}
	if want := (sizes{Fine: true, Row: 44, Head: 36, Button: 36, Search: 36, Overlay: 36, CompactRow: 44, CompactHead: 36}); got != want {
		t.Errorf("a dense page on a mouse = %+v, want rows 44 and 36px controls, outside the row too", got)
	}
	got = measure(plain.URL, mouse)
	if want := (sizes{Fine: true, Row: 52, Head: 44, Button: 44, Search: 44, Overlay: 44, CompactRow: 44, CompactHead: 36}); got != want {
		t.Errorf("a plain page = %+v, want rows 52 and 44px controls, and the compact frame at its own height", got)
	}
	got = measure(dense.URL, chromedp.EmulateViewport(390, 800), emulation.SetTouchEmulationEnabled(true).WithMaxTouchPoints(5))
	if got.Fine {
		t.Fatalf("touch emulation still reports a fine pointer: %+v", got)
	}
	if got.Row != 52 || got.Button != 44 || got.Search != 44 || got.Overlay != 44 {
		t.Errorf("a dense page on a touch screen = %+v, want the 44px targets", got)
	}
}
