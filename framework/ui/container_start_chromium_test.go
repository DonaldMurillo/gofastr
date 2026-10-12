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

// A Start container sits on its frame's start edge with no gutter of
// its own, at every breakpoint: its text lines up with a sibling that
// is not in a container. The default still centers inside its gutter.
func TestContainerStartPinsToFrameEdge(t *testing.T) {
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
	body := `<div id="frame" style="padding: 24px">` +
		`<p id="sibling">Header</p>` +
		string(ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow, Start: true, ID: "start"}, render.Text("x"))) +
		string(ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow, ID: "centered"}, render.Text("x"))) +
		`</div>`
	for _, width := range []int{390, 800, 1440} {
		srv := themeTestPageWithHead(t, head, body)
		ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(int64(width), 800))
		var got map[string]float64
		probe := `(() => {
			const r = id => document.getElementById(id).getBoundingClientRect();
			const pad = id => parseFloat(getComputedStyle(document.getElementById(id)).paddingLeft);
			return {sibling: r("sibling").left, start: r("start").left, startPad: pad("start"),
				centered: r("centered").left, centeredRight: innerWidth - r("centered").right};
		})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
			t.Fatalf("probe: %v", err)
		}
		if got["start"] != got["sibling"] || got["startPad"] != 0 {
			t.Errorf("%dpx: the Start container sits at %v with %vpx gutter; its sibling's text starts at %v",
				width, got["start"], got["startPad"], got["sibling"])
		}
		if width == 1440 && got["centered"] <= got["sibling"] {
			t.Errorf("%dpx: the default container no longer centers: %v", width, got)
		}
	}
}
