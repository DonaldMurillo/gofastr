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

// --ui-step-rail-top is the gap above the rail. Under BelowHeader that
// gap sits below the header, so setting the knob never drops the
// header's height and slides the rail behind a sticky header.
func TestStepRailTopKnobBelowHeader(t *testing.T) {
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
	items := []ui.StepRailItem{{Number: "01", Anchor: "a", Label: "A"}}
	rail := func(id string, below bool, knob string) string {
		attrs := map[string]string{"id": id}
		if knob != "" {
			attrs["style"] = "--ui-step-rail-top: " + knob
		}
		return string(render.Tag("div", attrs, ui.StepRail(ui.StepRailConfig{Items: items, ActiveIndex: -1, BelowHeader: below})))
	}
	body := rail("plain", false, "") + rail("plainknob", false, "16px") + rail("below", true, "") + rail("belowknob", true, "16px")
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1200, 800))
	var got map[string]float64
	probe := `(() => {
		const out = {};
		for (const id of ['plain', 'plainknob', 'below', 'belowknob']) {
			out[id] = parseFloat(getComputedStyle(document.querySelector('#' + id + ' [data-cui-comp="ui-step-rail"]')).top);
		}
		const h = document.createElement('div');
		h.style.height = 'var(--size-header-height, 56px)';
		document.body.appendChild(h);
		out.header = h.getBoundingClientRect().height;
		const s = document.createElement('div');
		s.style.height = 'var(--spacing-xl, 24px)';
		document.body.appendChild(s);
		out.gap = s.getBoundingClientRect().height;
		return out;
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	want := map[string]float64{
		"plain":     got["gap"],
		"plainknob": 16,
		"below":     got["header"] + got["gap"],
		"belowknob": got["header"] + 16,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s rail sticks at %vpx; want %vpx (header %vpx)", id, got[id], w, got["header"])
		}
	}
}
