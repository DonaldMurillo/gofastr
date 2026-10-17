package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit/nojstest"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestNoJS_AllPagesWork loads every page the axe gate scans with script
// execution off: the server's HTML alone must carry the content, the
// links and the forms.
func TestNoJS_AllPagesWork(t *testing.T) {
	if testing.Short() {
		t.Skip("no-JS e2e: -short")
	}
	base := startE2EServer(t)
	pages := axePages(t)
	browser := chromedptest.Context(t, chromedptest.WindowSize(1280, 800), chromedptest.Timeout(10*60e9))
	tab, cancel := nojstest.NewTab(t, browser)
	defer cancel()
	for _, p := range pages {
		ps, err := nojstest.Check(tab, base+p)
		if err != nil {
			t.Errorf("no-JS %s: %v", p, err)
			continue
		}
		nojstest.Report(t, p, ps)
	}
	t.Logf("no-JS: checked %d pages", len(pages))
}
