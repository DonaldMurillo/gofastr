package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestE2EPollingDotLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var dotCount, labelCount int
	var role string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pollingindicator"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-polling-indicator__dot').length`, &dotCount),
		chromedp.Evaluate(`document.querySelectorAll('.ui-polling-indicator__label').length`, &labelCount),
		chromedp.Evaluate(`document.querySelector('.ui-polling-indicator')?.getAttribute('role') || ''`, &role),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if dotCount < 3 || labelCount < 3 {
		t.Errorf("expected ≥3 dots and ≥3 labels (default, custom, paused), got dot=%d label=%d", dotCount, labelCount)
	}
	if role != "status" {
		t.Errorf("expected role=\"status\", got %q", role)
	}
}

func TestE2EPollingPaused(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pausedCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pollingindicator"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-polling-indicator--paused').length`, &pausedCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pausedCount < 1 {
		t.Error("expected at least one .ui-polling-indicator--paused on the demo page")
	}
}
