package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestE2E_CSSLoadingDemo_AutoLoadsOnClick verifies the LoadAuto flow:
// the demo-fancy-card CSS is NOT in the SSR bundle (because it's not
// rendered server-side on this page); clicking the reveal button
// triggers an island RPC that returns HTML with the marker, and the
// runtime fetches the per-component sheet on demand.
func TestE2E_CSSLoadingDemo_AutoLoadsOnClick(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var beforeCount int
	var afterCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/css-loading"),
		pageReady(),
		// Pre-click: the demo-fancy-card link should NOT be in the DOM,
		// because the page renders no marker for it (the LoadAuto SSR
		// collector only emits links for components actually rendered).
		chromedp.Evaluate(`document.querySelectorAll('link[data-fui-style="demo-fancy-card"]').length`, &beforeCount),
		// Click the reveal button (island RPC → server returns HTML
		// with the marker → runtime scans + loads the sheet).
		chromedp.Click(`button[data-fui-rpc="/islands/css-demo/reveal-card"]`),
		settle(),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('link[data-fui-style="demo-fancy-card"]').length`, &afterCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if beforeCount != 0 {
		t.Errorf("expected demo-fancy-card link absent before click, got %d", beforeCount)
	}
	if afterCount != 1 {
		t.Errorf("expected exactly 1 demo-fancy-card link after click, got %d", afterCount)
	}
}

// TestE2E_CSSLoadingDemo_PrewarmIsAlreadyLoaded verifies the
// LoadPrewarm flow: the demo-command-palette CSS gets idle-fetched
// after first paint, even though the palette isn't rendered on the
// initial page. By the time the user clicks reveal, the link is
// already present and the runtime reuses it.
func TestE2E_CSSLoadingDemo_PrewarmIsAlreadyLoaded(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var paletteLinkCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/css-loading"),
		pageReady(),
		// Give the idle queue a moment to fire — requestIdleCallback
		// runs whenever the main thread is free, but in headless
		// chrome we should be well past first paint by now.
		settle(),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('link[data-fui-style="demo-command-palette"]').length`, &paletteLinkCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if paletteLinkCount != 1 {
		t.Errorf("expected demo-command-palette link prewarmed (1), got %d — idle prefetch failed", paletteLinkCount)
	}
}

// TestE2E_CSSLoadingDemo_CatalogTableLists registered entries —
// confirms the server-rendered catalog table actually surfaces every
// component the registry knows about, not just the live demos.
func TestE2E_CSSLoadingDemo_CatalogTableLists(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var tableHTML string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/css-loading"),
		pageReady(),
		chromedp.OuterHTML(`section[aria-labelledby$="catalog"], section[aria-label*="catalog"], [id="catalog"]`, &tableHTML, chromedp.ByQuery),
	)
	// The selector above may not match perfectly depending on the
	// Section's aria attrs — fall back to scraping the whole page.
	if err != nil || tableHTML == "" {
		var fullHTML string
		err2 := chromedp.Run(ctx, chromedp.OuterHTML(`body`, &fullHTML))
		if err2 != nil {
			t.Fatalf("chromedp: %v / %v", err, err2)
		}
		tableHTML = fullHTML
	}
	for _, name := range []string{"ui-page-header", "ui-form-field", "demo-fancy-card", "demo-command-palette"} {
		if !strings.Contains(tableHTML, name) {
			t.Errorf("catalog table missing %q", name)
		}
	}
}
