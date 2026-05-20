package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestE2EIconGallery(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var svgCount int
	var sample string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		// Every Icon emits one <svg class="ui-icon ...">.
		chromedp.Evaluate(`document.querySelectorAll('svg.ui-icon').length`, &svgCount),
		// One built-in icon ("check") must include the currentColor stroke.
		chromedp.Evaluate(`(function() {
			var s = document.querySelector('svg.ui-icon');
			return s ? s.outerHTML : '';
		})()`, &sample),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if svgCount < 10 {
		t.Errorf("expected at least 10 ui-icon SVGs (built-in gallery), got %d", svgCount)
	}
	if !strings.Contains(sample, `stroke="currentColor"`) {
		t.Errorf("expected currentColor stroke, got: %s", sample)
	}
}

func TestE2EIconAriaHidden(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			// First gallery icon is decorative — no AriaLabel — should be aria-hidden.
			var s = document.querySelector('svg.ui-icon');
			return s && s.getAttribute('aria-hidden') === 'true';
		})()`, &hidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hidden {
		t.Error("expected default Icon to be aria-hidden=\"true\"")
	}
}

// Regression: a previous version registered "custom-square" inside the
// Icon screen's Render(), so the very first request to /components/icon
// rendered the custom-icon demo as empty — the registry only had it
// after a prior page load had mutated global state. Fixed by moving the
// RegisterIcon call to package init.
func TestE2ECustomIconAtInit(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var rectCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		// The "Registering custom icons" demo embeds an SVG with a
		// <rect> body. If the icon failed to render, the rect count is 0.
		chromedp.Evaluate(`document.querySelectorAll('svg.ui-icon rect').length`, &rectCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if rectCount < 1 {
		t.Error("custom Icon was not rendered on first request — the demo's RegisterIcon must run at init time, not inside Render()")
	}
}

func TestE2EIconLabeledImg(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var labeled bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var svgs = document.querySelectorAll('svg.ui-icon[role="img"]');
			for (var i = 0; i < svgs.length; i++) {
				if (svgs[i].getAttribute('aria-label')) return true;
			}
			return false;
		})()`, &labeled),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !labeled {
		t.Error("expected at least one Icon with role=\"img\" and aria-label on the demo page")
	}
}
