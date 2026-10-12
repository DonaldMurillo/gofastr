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

// A stacked DetailList between two text fields lines up with them: its
// label sits where theirs do and reads like theirs, and its value box
// has an input's edges and height, so a read-only value in a form does
// not jut out of the column.
func TestDetailListStackedLinesUpWithFields(t *testing.T) {
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
	body := ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.TextField(ui.TextFieldConfig{Name: "number", Label: "Number", ID: "number", Value: "INV-1010"}),
		ui.DetailList(ui.DetailListConfig{Stacked: true, ExtraAttrs: map[string]string{"data-test": "ro"},
			Items: []ui.DetailItem{{Label: "Status", Value: render.Text("Open")}}}),
	)
	for _, width := range []int64{1280, 390} {
		srv := themeTestPageWithHead(t, head, `<div style="padding: 24px; max-inline-size: 40rem">`+string(body)+`</div>`)
		ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(width, 800), prefersScheme("light"))
		var got map[string]float64
		geom := `(() => {
			const input = document.getElementById("number").getBoundingClientRect();
			const label = document.querySelector('label[for="number"]');
			const lr = label.getBoundingClientRect();
			const ro = document.querySelector('[data-test="ro"]');
			const term = ro.querySelector("dt"), val = ro.querySelector("dd");
			const tr = term.getBoundingClientRect(), vr = val.getBoundingClientRect();
			const w = el => getComputedStyle(el).fontWeight;
			return {
				valueLeft: vr.left - input.left, valueRight: vr.right - input.right,
				valueHeight: vr.height - input.height, labelLeft: tr.left - lr.left,
				labelGap: (vr.top - tr.bottom) - (input.top - lr.bottom),
				sameWeight: w(term) === w(label) ? 1 : 0,
				sameColor: getComputedStyle(term).color === getComputedStyle(label).color ? 1 : 0,
			};
		})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(geom, &got)); err != nil {
			t.Fatalf("geometry: %v", err)
		}
		for _, k := range []string{"valueLeft", "valueRight", "valueHeight", "labelLeft", "labelGap"} {
			if got[k] > 1 || got[k] < -1 {
				t.Errorf("at %dpx the read-only %s is off by %vpx: %v", width, k, got[k], got)
			}
		}
		if got["sameWeight"] != 1 || got["sameColor"] != 1 {
			t.Errorf("at %dpx the read-only label does not read like a field label: %v", width, got)
		}
	}
}
