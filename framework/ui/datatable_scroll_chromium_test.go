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

// A table wider than a phone scrolls inside its own box. A cell's
// absolutely positioned part (a visually hidden label, a row menu's
// panel) takes its containing block from the nearest positioned
// ancestor; with none inside the scroll box it escaped the clip and
// widened the page from where it sat in the wide table.
func TestDataTableScrollKeepsPageWidth(t *testing.T) {
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
	var cols []ui.Column
	cells := map[string]render.HTML{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
		cols = append(cols, ui.Column{Key: k, Header: "Column " + k})
		cells[k] = render.HTML(`<span style="white-space: nowrap">a value wide enough</span>` +
			`<span class="fui-visually-hidden">hidden label</span>`)
	}
	table := ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: []ui.Row{{Cells: cells}}})
	srv := themeTestPageWithHead(t, head, string(table))
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(390, 844), prefersScheme("light"))

	var got map[string]float64
	geom := `(() => ({
		page: document.documentElement.scrollWidth,
		view: document.documentElement.clientWidth,
		table: document.querySelector(".fui-data-table__scroll").scrollWidth,
	}))()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(geom, &got)); err != nil {
		t.Fatalf("geometry: %v", err)
	}
	if got["table"] <= got["view"] {
		t.Fatalf("the fixture table does not overflow the phone: %v", got)
	}
	if got["page"] > got["view"]+1 {
		t.Errorf("the page scrolls sideways: document %vpx in a %vpx viewport", got["page"], got["view"])
	}
}
