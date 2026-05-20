package main

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Mid-flight a11y: button must carry aria-busy=true and disabled
// while the RPC is in flight, so screen readers announce the state
// change and a second click can't fire a duplicate.
func TestE2EOptimisticPendingA11y(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pendingAriaBusy string
	var pendingDisabled bool
	var finalAriaBusy string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/optimisticaction"),
		pageReady(),
		chromedp.Click(`[data-fui-optimistic-endpoint="/demo/optimistic-slow"]`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-optimistic-endpoint="/demo/optimistic-slow"]')?.getAttribute('aria-busy') || ''`, &pendingAriaBusy),
		chromedp.Evaluate(`document.querySelector('[data-fui-optimistic-endpoint="/demo/optimistic-slow"]')?.disabled === true`, &pendingDisabled),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-optimistic-endpoint="/demo/optimistic-slow"]')?.getAttribute('aria-busy') || ''`, &finalAriaBusy),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pendingAriaBusy != "true" {
		t.Errorf("expected aria-busy=true during pending, got %q", pendingAriaBusy)
	}
	if !pendingDisabled {
		t.Error("expected button disabled during pending")
	}
	if finalAriaBusy == "true" {
		t.Errorf("expected aria-busy cleared after commit, got %q", finalAriaBusy)
	}
}

// CSRF: when a <meta name="csrf-token"> is on the page, the runtime
// must forward its value as X-CSRF-Token on every state-changing
// fetch. Apps verify the token server-side; this test verifies the
// client sends it.
func TestE2EOptimisticCSRFHeader(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var recorded string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/optimisticaction"),
		pageReady(),
		chromedp.Click(`[data-fui-optimistic-endpoint="/demo/csrf-record"]`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(async () => { var r = await fetch('/demo/csrf-last'); return await r.text(); })()`, &recorded,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) },
		),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if recorded != "demo-csrf-token-abc123" {
		t.Errorf("expected X-CSRF-Token forwarded from <meta>, got %q", recorded)
	}
}

func TestE2EOptimisticSuccess(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var stateBefore, stateAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/optimisticaction"),
		pageReady(),
		// First demo button hits /demo/optimistic-success (204).
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-optimistic-action"]')?.getAttribute('data-state')`, &stateBefore),
		chromedp.Click(`[data-fui-comp="ui-optimistic-action"][data-fui-optimistic-endpoint="/demo/optimistic-success"]`, chromedp.ByQuery),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-optimistic-action"][data-fui-optimistic-endpoint="/demo/optimistic-success"]')?.getAttribute('data-state')`, &stateAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if stateBefore != "idle" {
		t.Errorf("expected initial data-state=idle, got %q", stateBefore)
	}
	if stateAfter != "committed" {
		t.Errorf("expected data-state=committed after 204, got %q", stateAfter)
	}
}

func TestE2EOptimisticRollback(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var stateAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/optimisticaction"),
		pageReady(),
		chromedp.Click(`[data-fui-optimistic-endpoint="/demo/optimistic-failure"]`, chromedp.ByQuery),
		// Wait long enough for the 500 response + the 600ms rollback timer.
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelector('[data-fui-optimistic-endpoint="/demo/optimistic-failure"]')?.getAttribute('data-state')`, &stateAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if stateAfter != "idle" {
		t.Errorf("expected data-state=idle after rollback, got %q", stateAfter)
	}
}
