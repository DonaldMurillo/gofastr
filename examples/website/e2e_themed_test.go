package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestE2E_Themed_OverridesCascadeIntoSubtree confirms that
// ui.Themed(Dark, …) actually changes computed colors inside the
// wrapped subtree while leaving the rest of the page alone.
//
// The /framework-ui/themed screen renders the same component tree
// twice side-by-side; only the right panel is wrapped with Themed.
// The body bg color from the wrapped <section> must differ from
// the body bg color of the unwrapped one.
func TestE2E_Themed_OverridesCascadeIntoSubtree(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var got map[string]string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/themed"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            // Two .themed-demo__panel siblings: [0] is default
            // theme, [1] wraps its sample in ui.Themed(Dark, ...).
            const panels = document.querySelectorAll('.themed-demo__panel');
            if (panels.length < 2) return {error: "expected 2 panels, got " + panels.length};
            // Find a .ui-section inside each panel and compare bg.
            const lightSection = panels[0].querySelector('section.ui-section');
            const darkSection = panels[1].querySelector('section.ui-section');
            if (!lightSection || !darkSection) return {error: "missing sections"};
            return {
                lightText: getComputedStyle(lightSection).color,
                darkText:  getComputedStyle(darkSection).color,
                hasThemeWrap: panels[1].querySelector('[class*="fui-theme-"]') !== null ? "yes" : "no",
            };
        })()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got["error"] != "" {
		t.Fatalf("setup: %s", got["error"])
	}
	if got["hasThemeWrap"] != "yes" {
		t.Errorf("right panel should contain a .fui-theme-<hash> wrapper element")
	}
	if got["lightText"] == got["darkText"] {
		t.Errorf("Themed override didn't cascade: section text color is the same in both panels (%s)", got["lightText"])
	}
}

// TestE2E_Themed_RegistersBlockInAppCSS confirms that the
// app.css response contains a .fui-theme-<hash> rule for the
// registered Dark override.
func TestE2E_Themed_RegistersBlockInAppCSS(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var body string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/__gofastr/app.css"),
		chromedp.Text(`body`, &body),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(body, ".fui-theme-") {
		t.Errorf("app.css should contain a .fui-theme-<hash> block once an override is registered:\nfirst 800 bytes:\n%s", head(body, 800))
	}
}

func head(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
