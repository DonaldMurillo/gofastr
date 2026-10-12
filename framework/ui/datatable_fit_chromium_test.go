package ui_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A list table's chrome: the select column fits its checkbox, a sort
// header's label sits level with the values under it at either edge,
// and the select-all box draws the kit's checkbox, filled when mixed.
func TestDataTableFitAndHeaderAlignment(t *testing.T) {
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
	box := func(v string) render.HTML {
		return ui.Checkbox(ui.ToggleConfig{Name: "ids", Value: v, ID: "sel-" + v, Label: "Select " + v, LabelHidden: true})
	}
	tbl := ui.DataTable(ui.DataTableConfig{
		Path: "/x",
		Columns: []ui.Column{
			{Key: "pick", SelectAll: "ids", Fit: true},
			{Key: "name", Header: "Name", Sortable: true},
			{Key: "amount", Header: "Amount", Sortable: true, Align: "end"},
			{Key: "menu", Fit: true, Align: "end"},
		},
		Rows: []ui.Row{
			{ID: "r1", Cells: map[string]render.HTML{"pick": box("1"), "name": render.Text("INV-1010"), "amount": render.Text("$99.00"), "menu": render.Text("…")}},
			{ID: "r2", Cells: map[string]render.HTML{"pick": box("2"), "name": render.Text("INV-1011"), "amount": render.Text("$299.00"), "menu": render.Text("…")}},
		},
	})
	body := `<div style="width: 900px; padding: 16px">` + string(tbl) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1000, 600))
	var got map[string]any
	probe := `(() => {
		const r = e => e.getBoundingClientRect();
		const text = el => { const g = document.createRange(); g.selectNodeContents(el); return g.getBoundingClientRect(); };
		const ths = document.querySelectorAll("thead th"), tds = document.querySelectorAll("tbody tr:first-child td");
		const all = document.querySelector("[data-hui-table-select-all]");
		const row = document.querySelector("#sel-1");
		const a = ths[2].querySelector("a");
		return {
			pickW: r(ths[0]).width, nameW: r(ths[1]).width,
			nameHead: text(ths[1].querySelector("a")).left, nameCell: text(tds[1]).left,
			amountHead: r(a).right - parseFloat(getComputedStyle(a).paddingRight), amountCell: text(tds[2]).right,
			allW: r(all).width, rowW: r(row).width, allRadius: getComputedStyle(all).borderTopLeftRadius,
			rowRadius: getComputedStyle(row).borderTopLeftRadius,
			mixed: getComputedStyle(all).backgroundImage, mixedBorder: getComputedStyle(all).borderTopColor,
			checkedBorder: getComputedStyle(document.querySelector("#sel-2")).borderTopColor,
			unchecked: getComputedStyle(row).backgroundImage, uncheckedBorder: getComputedStyle(row).borderTopColor,
		};
	})()`
	// The box transitions its fill, so the probe reads after it settles.
	set := `document.querySelector("[data-hui-table-select-all]").indeterminate = true; document.querySelector("#sel-2").checked = true`
	if err := chromedp.Run(ctx, chromedp.Evaluate(set, nil), chromedp.Sleep(400*time.Millisecond), chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	f := func(k string) float64 { v, _ := got[k].(float64); return v }
	if f("pickW") > 60 {
		t.Errorf("the fitted select column took spare width: %v", got)
	}
	if math.Abs(f("nameHead")-f("nameCell")) > 1 {
		t.Errorf("a start-aligned sort label sits off its values: %v", got)
	}
	if math.Abs(f("amountHead")-f("amountCell")) > 1 {
		t.Errorf("an end-aligned sort control sits off its values' end: %v", got)
	}
	if f("allW") != f("rowW") || got["allRadius"] != got["rowRadius"] {
		t.Errorf("the select-all box is not the rows' checkbox: %v", got)
	}
	if !strings.Contains(fmt.Sprint(got["mixed"]), "linear-gradient") || got["unchecked"] != "none" || got["mixedBorder"] != got["checkedBorder"] || got["mixedBorder"] == got["uncheckedBorder"] {
		t.Errorf("a mixed select-all is not filled: %v", got)
	}
}

func TestDataTableRefusesFitWithWrap(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "Fit and Wrap") {
			t.Fatalf("a column with Fit and Wrap rendered: %v", r)
		}
	}()
	ui.DataTable(ui.DataTableConfig{Columns: []ui.Column{{Key: "note", Header: "Note", Fit: true, Wrap: true}}})
}
