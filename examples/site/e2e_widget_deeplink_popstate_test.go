package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Widget deep links round-trip through history: opening a deep-linked
// modal pushes its query onto the URL, and Back pops it closed again.
//
// This characterizes behavior that was previously wired up across three
// places in the core runtime — a boot-time index build, a second index
// build on SPA navigation, and a popstate listener. Deriving that state
// from the widget catalog on demand is a refactor, and this test is what
// makes it a refactor rather than a rewrite: if the sync stops running
// on popstate, the modal stays open here.
//
// Forward is deliberately NOT asserted. It does not restore the modal
// today, and the cause is older and wider than this file: the SPA
// popstate handler keys on pathname+search, so a widget deep link —
// which changes only the query — reads as a page navigation and
// re-fetches the screen, discarding the client-mounted widget. Fixing
// that means teaching the router which query parameters describe
// in-page state, which is its own change.
func TestE2E_Modal_DeepLinkPopstateCloses(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	const modal = `!!document.querySelector('[data-fui-widget="site-demo-modal"]')`

	var urlAfterOpen, urlAfterBack string
	var openAfterOpen, openAfterBack bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal"),
		pageReady(),
		chromedp.WaitVisible(`button[data-fui-open="site-demo-modal"][data-fui-deeplink]`),

		// The deep-linked trigger opens the modal AND records it.
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-modal"][data-fui-deeplink]').click()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(`location.search`, &urlAfterOpen),
		chromedp.Evaluate(modal, &openAfterOpen),

		// Back closes it.
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(`location.search`, &urlAfterBack),
		chromedp.Evaluate(modal, &openAfterBack),
	); err != nil {
		t.Fatal(err)
	}

	if !openAfterOpen {
		t.Fatal("deep-linked trigger did not open the modal")
	}
	if urlAfterOpen == "" {
		t.Errorf("opening did not push the deep link: search=%q", urlAfterOpen)
	}
	if openAfterBack {
		t.Error("Back did not close the deep-linked modal")
	}
	if urlAfterBack == urlAfterOpen {
		t.Errorf("Back did not change the URL (still %q)", urlAfterBack)
	}
}
