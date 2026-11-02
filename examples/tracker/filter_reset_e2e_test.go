package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestTrackerFilterFollowsURLE2E: the issue list shows what its URL
// asks for after a client navigation that changes the filter. The list
// lived in the project layer, which the router keeps across one
// project's pages and never re-renders, while it read ?filter=. Reset
// (and the sidebar's project link, from a filtered issue) moved the URL
// to /projects/billing and left the filtered list and the old filter
// text on screen: a refresh of the same URL showed a different page.
func TestTrackerFilterFollowsURLE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	visibleRows := `Array.from(document.querySelectorAll('.fui-card[data-key]')).filter(r => r.offsetParent !== null).length`
	wantAll := func(step string) {
		t.Helper()
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Poll(
			`location.search === '' && `+visibleRows+` === 8 && document.querySelector('#filter-search-filter').value === ''`,
			&ok, chromedp.WithPollingTimeout(10*time.Second))); err != nil || !ok {
			var rows int
			var val, loc string
			_ = chromedp.Run(ctx, chromedp.Evaluate(visibleRows, &rows),
				chromedp.Evaluate(`document.querySelector('#filter-search-filter').value`, &val),
				chromedp.Evaluate(`location.pathname + location.search`, &loc))
			t.Fatalf("%s: at %s the list shows %d rows with filter %q, want all 8 and an empty filter", step, loc, rows, val)
		}
	}

	// Reset from a filtered list.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing?filter=BIL-42"),
		chromedp.WaitVisible(`#filter-search-filter[value="BIL-42"]`, chromedp.ByQuery),
		chromedp.Click(`[data-cui-comp="ui-filter-toolbar"] a[href="/projects/billing"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("reset: %v", err)
	}
	wantAll("Reset")

	// The sidebar's project link from a filtered issue.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/42?filter=BIL-42"),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
		chromedp.Click(`.fui-content-row__nav nav a[href="/projects/billing"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("sidebar: %v", err)
	}
	wantAll("sidebar Billing")

	// Moving between a filtered list's issues still keeps the filter.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing?filter=retry"),
		chromedp.WaitVisible(`#filter-search-filter[value="retry"]`, chromedp.ByQuery),
		chromedp.Click(`a.fui-card[data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("filtered issue: %v", err)
	}
	var rows int
	var val string
	if err := chromedp.Run(ctx, chromedp.Evaluate(visibleRows, &rows),
		chromedp.Evaluate(`document.querySelector('#filter-search-filter').value`, &val)); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || val != "retry" {
		t.Fatalf("on a filtered issue the list shows %d rows with filter %q, want 1 and retry", rows, val)
	}
}
