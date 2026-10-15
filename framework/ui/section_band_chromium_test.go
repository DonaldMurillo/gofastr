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

// A page band (Band, or an eyebrow) sets its own heading at display
// size, an Overline section its own as a small label; a plain section
// nested in either's body keeps the in-app size.
func TestBandHeadingSkipsNestedSection(t *testing.T) {
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
	nested := func(id string) render.HTML {
		return ui.Section(ui.SectionConfig{Heading: id}, render.Text("x"))
	}
	body := string(ui.Section(ui.SectionConfig{Heading: "plain"}, render.Text("x"))) +
		string(ui.Section(ui.SectionConfig{Heading: "band", Band: true}, nested("in-band"))) +
		string(ui.Section(ui.SectionConfig{Heading: "brow", Eyebrow: "01"}, nested("in-brow"))) +
		string(ui.Section(ui.SectionConfig{Heading: "over", Overline: true}, nested("in-over")))
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL)
	var got map[string]float64
	probe := `(() => {
		const out = {};
		for (const h of document.querySelectorAll('.fui-section__heading')) out[h.id] = parseFloat(getComputedStyle(h).fontSize);
		return out;
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	for _, id := range []string{"band-title", "brow-title"} {
		if got[id] <= got["plain-title"] {
			t.Errorf("%s heading is %vpx; want display size above the plain %vpx", id, got[id], got["plain-title"])
		}
	}
	if got["over-title"] >= got["plain-title"] {
		t.Errorf("overline heading is %vpx; want the small label below the plain %vpx", got["over-title"], got["plain-title"])
	}
	for _, id := range []string{"in-band-title", "in-brow-title", "in-over-title"} {
		if got[id] != got["plain-title"] {
			t.Errorf("%s heading is %vpx inside a band; want the plain section's %vpx", id, got[id], got["plain-title"])
		}
	}
}
