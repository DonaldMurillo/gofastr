package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestE2EScrollSpyLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var wrapperFound, anchorCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/scrollspy"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-scrollspy]').length`, &wrapperFound),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-scrollspy] a[href^="#"]').length`, &anchorCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if wrapperFound < 1 {
		t.Error("expected [data-fui-scrollspy] wrapper on /components/scrollspy")
	}
	if anchorCount < 5 {
		t.Errorf("expected ≥5 anchor links (demo has 5 sections), got %d", anchorCount)
	}
}

// Bootstrap iterates targets in DOM order (not anchor order) so the
// initial active selection on page-land picks the first DOM section
// whose top is above the viewport midline. Reversed-nav demo lists
// Conclusion first; without the DOM-order sort, the bootstrap fell
// back to targets[0] which was the conclusion anchor — wrong.
func TestE2EScrollSpyReversedNavBootstrap(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var active string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/scrollspy"),
		pageReady(),
		// No scroll — read the bootstrap-time active state directly.
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var wraps = document.querySelectorAll('[data-fui-scrollspy]');
			var w = wraps[wraps.length - 1]; // reversed-nav wrap is last
			if (!w) return '';
			var a = w.querySelector('a.is-active');
			return a ? a.getAttribute('href') : '';
		})()`, &active),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// Expected: the DOM-topmost section's anchor (#rev-spy-intro) —
	// NOT the first nav item (#rev-spy-conclusion).
	if active != "#rev-spy-intro" {
		t.Errorf("expected bootstrap to pick DOM-topmost (#rev-spy-intro) on reversed nav, got %q", active)
	}
}

func TestE2EScrollSpyOnScroll(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var initialActive, scrolledActive string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/scrollspy"),
		pageReady(),
		// Wait for the runtime module to load + IntersectionObserver
		// to fire its initial pass. Scroll near the top so the first
		// section is active.
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var a = document.querySelector('[data-fui-scrollspy] a.is-active');
			return a ? a.getAttribute('href') : '';
		})()`, &initialActive),
		// Scroll the page to put #spy-config in the upper viewport
		// (third section in the demo).
		chromedp.Evaluate(`(function(){
			var t = document.querySelector('#spy-config');
			if (t) t.scrollIntoView({block: 'start'});
			return true;
		})()`, nil),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`(function(){
			var a = document.querySelector('[data-fui-scrollspy] a.is-active');
			return a ? a.getAttribute('href') : '';
		})()`, &scrolledActive),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if initialActive == "" {
		t.Error("expected an initial active anchor (first section in view)")
	}
	// After scrolling to #spy-config, the active anchor should match.
	if scrolledActive != "#spy-config" {
		t.Errorf("expected #spy-config to be active after scroll, got %q (initial=%q)",
			scrolledActive, initialActive)
	}
}

