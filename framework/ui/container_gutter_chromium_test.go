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

// A page-level container (Pad: Page or End) keeps the page gutter at
// every width, whatever its measure, so its text starts on the edge a
// header or footer band drawn with --size-page-gutter uses. A Start
// container still owns no gutter.
func TestPageContainerUsesPageGutter(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>body{margin:0}" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	widths := map[string]ui.ContainerWidth{"default": "", "narrow": ui.ContainerNarrow, "wide": ui.ContainerWide, "page": ui.ContainerPage}
	var body strings.Builder
	for id, w := range widths {
		body.WriteString(string(ui.Container(ui.ContainerConfig{Width: w, Pad: ui.ContainerPadPage, ID: id}, render.Text("x"))))
	}
	body.WriteString(string(ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadEnd, ID: "end"}, render.Text("x"))))
	body.WriteString(string(ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow, Pad: ui.ContainerPadPage, Start: true, ID: "start"}, render.Text("x"))))
	// Inside an app frame's padded content area the frame's padding is
	// the gutter; a page container there adds none of its own.
	// The primary slot is the layout outlet (l.Primary()), a .layout-content.
	body.WriteString(string(ui.ContentRow(ui.ContentRowConfig{Sidebar: render.Text("nav")},
		render.Tag("div", map[string]string{"class": "layout-content"},
			ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadPage, ID: "framed"}, render.Text("x"))))))
	for _, vw := range []int{390, 800, 1440} {
		srv := themeTestPageWithHead(t, head, body.String())
		ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(int64(vw), 800))
		var got map[string]float64
		probe := `(() => {
			const out = {};
			for (const el of document.querySelectorAll('[data-cui-comp="ui-container"]')) {
				out[el.id] = parseFloat(getComputedStyle(el).paddingLeft);
			}
			const g = document.createElement('div');
			g.style.width = 'var(--size-page-gutter, clamp(20px, 5vw, 32px))';
			document.body.appendChild(g);
			out.gutter = g.getBoundingClientRect().width;
			return out;
		})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
			t.Fatalf("probe: %v", err)
		}
		for _, id := range []string{"default", "narrow", "wide", "page", "end"} {
			if got[id] != got["gutter"] {
				t.Errorf("%dpx: %s page container pads %vpx inline; the page gutter is %vpx", vw, id, got[id], got["gutter"])
			}
		}
		if got["start"] != 0 {
			t.Errorf("%dpx: a Start page container pads %vpx inline; want 0", vw, got["start"])
		}
		if got["framed"] != 0 {
			t.Errorf("%dpx: a page container inside an app frame pads %vpx inline on top of the frame's padding; want 0", vw, got["framed"])
		}
	}
}
