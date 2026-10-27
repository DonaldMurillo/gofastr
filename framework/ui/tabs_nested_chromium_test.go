package ui_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// An outer strip's active index styles only its own tabs and panels:
// a Tabs nested in its second panel keeps showing its own first panel.
func TestNestedTabsKeepTheirOwnPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	inner := ui.Tabs(ui.TabsConfig{SignalName: "inner", Tabs: []ui.TabItem{
		{Label: "One", Content: `<p id="in0">inner one</p>`},
		{Label: "Two", Content: `<p id="in1">inner two</p>`},
	}})
	outer := ui.Tabs(ui.TabsConfig{SignalName: "outer", Tabs: []ui.TabItem{
		{Label: "A", Content: `<p>outer a</p>`},
		{Label: "B", Content: inner},
	}})
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	srv := themeTestPageWithHead(t, head, string(outer))
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		document.querySelector('[data-cui-signal="outer"]').setAttribute("data-active", "1");
		const shown = el => getComputedStyle(el.closest(".fui-tab-panel")).display !== "none";
		const tab = document.querySelectorAll('[data-cui-signal="inner"] .fui-tab');
		return {in0: shown(document.querySelector("#in0")), in1: shown(document.querySelector("#in1")),
			raised0: getComputedStyle(tab[0]).boxShadow !== "none", raised1: getComputedStyle(tab[1]).boxShadow !== "none"};
	})()`, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["in0"] != true || got["in1"] != false {
		t.Errorf("the outer strip's index chose the inner panel: %v", got)
	}
	if got["raised0"] != true || got["raised1"] != false {
		t.Errorf("the outer strip's index raised an inner tab: %v", got)
	}
}
