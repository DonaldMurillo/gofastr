package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A Dot badge draws a circle in its label's tone before the word; a
// plain one draws none.
func TestStatusBadgeDotDrawsCircle(t *testing.T) {
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
	body := `<p>` + string(ui.StatusBadge(ui.StatusBadgeConfig{Label: "Paid", Variant: ui.StatusSuccess, Dot: true, ID: "dot"})) +
		` ` + string(ui.StatusBadge(ui.StatusBadgeConfig{Label: "Paid", Variant: ui.StatusSuccess, ID: "plain"})) + `</p>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(600, 300))
	var got map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const dot = document.querySelector("#dot"), plain = document.querySelector("#plain");
		const b = getComputedStyle(dot, "::before");
		return {w: b.width, h: b.height, radius: b.borderTopLeftRadius, bg: b.backgroundColor, color: getComputedStyle(dot).color,
			plain: getComputedStyle(plain, "::before").content, wider: dot.getBoundingClientRect().width > plain.getBoundingClientRect().width};
	})()`, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["w"] != "6px" || got["h"] != "6px" || got["radius"] != "50%" || got["bg"] != got["color"] || got["wider"] != true {
		t.Errorf("a Dot badge drew no circle in its tone: %v", got)
	}
	if got["plain"] != "none" {
		t.Errorf("a plain badge drew a dot: %v", got)
	}
}
