package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestE2ESkeletonPresetsRender(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var card, row, avatar, footer int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skeleton"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-card').length`, &card),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-row').length`, &row),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-avatar').length`, &avatar),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-card__footer').length`, &footer),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if card < 1 {
		t.Errorf("expected at least 1 SkeletonCard, got %d", card)
	}
	if row < 2 {
		t.Errorf("expected ≥2 SkeletonRows (demo shows 3), got %d", row)
	}
	if avatar < 1 {
		t.Errorf("expected at least 1 SkeletonAvatar, got %d", avatar)
	}
	if footer < 1 {
		t.Errorf("expected SkeletonCard footer when ShowFooter=true, got %d", footer)
	}
}

func TestE2ESkeletonPresetsAriaHidden(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var allHidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skeleton"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var nodes = document.querySelectorAll('.ui-skeleton-card, .ui-skeleton-row, .ui-skeleton-avatar');
			if (nodes.length === 0) return false;
			for (var i = 0; i < nodes.length; i++) {
				if (nodes[i].getAttribute('aria-hidden') !== 'true') return false;
			}
			return true;
		})()`, &allHidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !allHidden {
		t.Error("every skeleton preset wrapper must be aria-hidden=\"true\"")
	}
}
