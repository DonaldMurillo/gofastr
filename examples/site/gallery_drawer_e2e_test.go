package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestDrawerTriggerDemoOpens(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(700, 900),
		chromedp.Navigate(base+"/components/sidebardrawertrigger"), pageReady(),
		chromedp.Click(`main [data-cui-open="ui-sidebar-drawer"]`),
		chromedp.Poll(`!!document.querySelector('[data-cui-widget="ui-sidebar-drawer"]:not([hidden])')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
}
