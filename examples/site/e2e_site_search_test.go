package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// TestE2E_PaletteSearchFindsSection drives the header palette the way a
// reader does: open it, type, and press Enter. The query only matches
// inside the theming page's body, so a hit proves the palette searches
// rendered page text, not its starter commands; Enter lands on the
// page and the section row carries the heading's anchor.
func TestE2E_PaletteSearchFindsSection(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var rows []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/docs/"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-cui-open="site-command-palette"]').click()`, nil),
		chromedp.WaitVisible(`#site-command-palette-input`, chromedp.ByID),
		chromedp.SendKeys(`#site-command-palette-input`, "darkcolors", chromedp.ByID),
		// A query that lands while the index is still building shows a
		// Loading… row first; results are the rows with a destination.
		chromedp.WaitVisible(`[data-search-result][data-cui-push-state]`, chromedp.ByQuery),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-search-result][data-cui-push-state]')].map(r => r.getAttribute('data-cui-push-state'))`, &rows),
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) < 2 || rows[0] != "/docs/theming" || !strings.HasPrefix(rows[1], "/docs/theming#") {
		t.Fatalf("result rows = %q, want /docs/theming then one of its sections", rows)
	}

	var path string
	if err := chromedp.Run(ctx,
		chromedp.KeyEvent(kb.Enter),
		chromedp.Poll(`location.pathname === '/docs/theming'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`location.pathname`, &path),
	); err != nil {
		t.Fatalf("enter on the first result: %v (at %s)", err, path)
	}
}
