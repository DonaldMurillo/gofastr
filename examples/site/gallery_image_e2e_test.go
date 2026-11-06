package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestImageDemoLoadsRaster(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/components/image"), pageReady(),
		chromedp.ScrollIntoView(`main img[width="320"][height="180"]`),
		chromedp.Poll(`(()=>{const image=document.querySelector('main img[width="320"][height="180"]');return image?.complete && image.naturalWidth===320 && image.naturalHeight===180})()`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
}
