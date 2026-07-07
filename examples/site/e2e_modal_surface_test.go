package main

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A plain preset.Modal must paint a visible panel behind its slot
// content: non-transparent surface background, padding, and rounded
// corners. The chrome groups all slots inside one .fui-panel and
// paints that (so multi-slot modals read as ONE dialog). Regression
// guard for the invisible-modal defect where slot content floated
// bare on the dimmed backdrop.
func TestE2E_ModalSlotPaintsSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var bg, padTop, radius string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-modal"]').click()`, nil),
		// Lazy-fetched widget needs time for the chrome request + mount.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`(() => {
            const s = document.querySelector('[data-fui-widget="site-demo-modal"] .fui-panel');
            return s ? getComputedStyle(s).backgroundColor : '';
        })()`, &bg),
		chromedp.Evaluate(`(() => {
            const s = document.querySelector('[data-fui-widget="site-demo-modal"] .fui-panel');
            return s ? getComputedStyle(s).paddingTop : '';
        })()`, &padTop),
		chromedp.Evaluate(`(() => {
            const s = document.querySelector('[data-fui-widget="site-demo-modal"] .fui-panel');
            return s ? getComputedStyle(s).borderTopLeftRadius : '';
        })()`, &radius),
	); err != nil {
		t.Fatalf("modal surface: %v", err)
	}

	if bg == "" {
		t.Fatal("modal panel not found after opening site-demo-modal")
	}
	if bg == "rgba(0, 0, 0, 0)" || bg == "transparent" {
		t.Errorf("panel background = %q — modal paints no panel (invisible-modal defect)", bg)
	}
	if padTop == "0px" {
		t.Error("panel padding-top = 0px, want a nonzero panel padding")
	}
	if radius == "0px" {
		t.Error("panel border-radius = 0px, want rounded panel corners")
	}
}

// A preset.BottomSheet must paint its panel on the widget root
// (surface background + shadow), like drawers do — not float slot
// text over the page.
func TestE2E_SheetPaintsSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var bg string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/bottomsheet"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-bottomsheet"]').click()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`(() => {
            const w = document.querySelector('[data-fui-widget="site-demo-bottomsheet"]');
            return w ? getComputedStyle(w).backgroundColor : '';
        })()`, &bg),
	); err != nil {
		t.Fatalf("sheet surface: %v", err)
	}
	if bg == "" {
		t.Fatal("bottom sheet not found after opening")
	}
	if bg == "rgba(0, 0, 0, 0)" || bg == "transparent" {
		t.Errorf("sheet background = %q — sheet paints no panel", bg)
	}
}
