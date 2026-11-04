package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestTagDemoDismisses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tag"), pageReady(),
		chromedp.Click(`a[aria-label="Remove beta"]`),
		chromedp.Poll(`!document.querySelector('a[aria-label="Remove beta"]') && document.querySelectorAll('main').length===1`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/components/tag?dismissed=beta"), pageReady(),
		chromedp.Poll(`!document.querySelector('a[aria-label="Remove beta"]')`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
		t.Fatal(err)
	}
}
