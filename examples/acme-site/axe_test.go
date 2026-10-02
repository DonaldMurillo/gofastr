package main

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/chromedp/chromedp"
)

// The axe-core gate over every page of the site, both color schemes.
// The reusable harness lives in framework/testkit/axetest; this file
// owns only the page list and the gate. Every successful Scan also
// records the page into the axe-coverage manifest, which uihost strict
// mode reads in dev — that is how the repo merges this app's coverage.
//
// Run ISOLATED, never parallel with other chromedp suites:
//
//	go test ./examples/acme-site/ -run TestAxeAcmeEveryScreen
//
// The allowlist starts EMPTY and the bar is it stays empty: this is a
// real site, every violation is fixed at its source.

// axeAllowlist names axe-core rule IDs deliberately skipped, with a
// justification. Empty: no skips.
var axeAllowlist = map[string]string{}

// axePages lists every public page: the marketing set, the help index
// and every article, derived from the same article slice the screens
// and the nav render from, so a new article cannot drift out of the
// gate.
func axePages() []string {
	pages := []string{"/", "/pricing", "/changelog", "/help"}
	for _, a := range helpArticles {
		pages = append(pages, "/help/"+a.Slug)
	}
	return pages
}

// TestAxeAcmeEveryScreen scans every page under both color schemes.
func TestAxeAcmeEveryScreen(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the app + headless Chrome")
	}
	base := acmeServe(t)
	browser := axetest.NewBrowser(t)
	for _, page := range axePages() {
		for _, scheme := range axetest.Schemes {
			t.Run(page+"/"+scheme, func(t *testing.T) {
				ctx, cancel := axetest.NewTab(t, browser)
				defer cancel()
				if err := chromedp.Run(ctx,
					chromedp.Navigate(base+page),
					chromedp.WaitReady("body", chromedp.ByQuery),
					chromedp.Sleep(250*time.Millisecond),
					axetest.Prepare(scheme),
					chromedp.Sleep(100*time.Millisecond),
				); err != nil {
					t.Fatalf("navigate %s: %v", page, err)
				}
				violations, err := axetest.Scan(ctx, scheme, axeAllowlist)
				if err != nil {
					t.Fatalf("scan %s (%s): %v", page, scheme, err)
				}
				for _, v := range violations {
					t.Errorf("%s (%s): %s — %s: %s", page, scheme, v.ID, v.Impact, v.Help)
					for _, n := range v.Nodes {
						t.Errorf("  node: %s", n.Target)
					}
				}
			})
		}
	}
}
