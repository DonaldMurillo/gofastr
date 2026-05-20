package main

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestE2ERetryBannerHiddenAtRest(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/networkretrybanner"),
		pageReady(),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-network-retry-banner"]')?.hasAttribute('hidden')`, &hidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hidden {
		t.Error("expected banner to be hidden by default")
	}
}

func TestE2ERetryBannerTripsAtThreshold(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenBefore, hiddenAfter bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/networkretrybanner"),
		pageReady(),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-network-retry-banner"]')?.hasAttribute('hidden')`, &hiddenBefore),
		// Click the demo trigger button 3 times (matches FailureThreshold).
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-network-retry-banner"]')?.hasAttribute('hidden')`, &hiddenAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hiddenBefore {
		t.Error("expected banner hidden before the threshold is hit")
	}
	if hiddenAfter {
		t.Error("expected banner visible after 3 failure reports (FailureThreshold=3)")
	}
}

// Two banners on the same page must both react to reportFailure().
// Pre-fix, the module-scope `banner` variable was overwritten by the
// last-mounted instance and only that one showed.
func TestE2ERetryBannerMultipleInstances(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var countTotal, hiddenCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/networkretrybanner"),
		pageReady(),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-network-retry-banner"]').length`, &countTotal),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-fui-comp="ui-network-retry-banner"]')).filter(b => b.hasAttribute('hidden')).length`, &hiddenCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if countTotal < 2 {
		t.Fatalf("expected 2 banners on the multi-instance demo, got %d", countTotal)
	}
	if hiddenCount != 0 {
		t.Errorf("expected ALL banners visible after 3 reportFailure() calls, got %d still hidden", hiddenCount)
	}
}

// Rapid retry clicks on a slow health endpoint must not fan out into
// N parallel fetches — the per-banner in-flight guard keeps it at 1.
func TestE2ERetryBannerNoConcurrentHealth(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var maxInflight string
	err := chromedp.Run(ctx,
		// Reset the high-water counter via raw HTTP before the test.
		chromedp.Navigate(base+"/components/networkretrybanner"),
		pageReady(),
		chromedp.Evaluate(`fetch('/demo/network-health-stats-reset', {method:'POST'}).then(()=>true)`, nil),
		chromedp.Sleep(200*time.Millisecond),
		// Trip the slow banner.
		chromedp.Evaluate(`(function(){window.__gofastr.networkStatus.reportFailure();window.__gofastr.networkStatus.reportFailure();window.__gofastr.networkStatus.reportFailure();return true;})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		// Smash the slow banner's retry button 5 times in a row.
		chromedp.Evaluate(`(function(){
			var b = document.querySelector('#retry-banner-slow [data-fui-network-retry-button]');
			for (var i=0;i<5;i++) b.click();
			return true;
		})()`, nil),
		// Wait long enough for all in-flight requests to drain.
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Evaluate(`(async () => { var r = await fetch('/demo/network-health-stats'); return await r.text(); })()`, &maxInflight,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) },
		),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if maxInflight != "1" {
		t.Errorf("expected max 1 concurrent /network-health-slow request, server saw %s", maxInflight)
	}
}

func TestE2ERetryBannerHidesAfterHealthCheck(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenAfterRetry bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/networkretrybanner"),
		pageReady(),
		chromedp.Sleep(500*time.Millisecond),
		// Trip the banner.
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-network-retry-demo-trigger]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		// Click Retry now → /demo/network-health returns 204 → reportRecovery → banner hides.
		chromedp.Click(`[data-fui-network-retry-button]`, chromedp.ByQuery),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-network-retry-banner"]')?.hasAttribute('hidden')`, &hiddenAfterRetry),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hiddenAfterRetry {
		t.Error("expected banner to hide after successful health check")
	}
}
