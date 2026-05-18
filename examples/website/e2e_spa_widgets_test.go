package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// SPA-navigation widget catalog refresh.
//
// Page-scoped widgets (.Pages("/route")) only appear in the catalog
// returned by /__gofastr/widgets?page=<route>. When the user arrives
// at the route via a partial-fetch SPA nav (no full page reload), the
// runtime needs to RE-FETCH the catalog so the widget's data-fui-open
// trigger actually opens it.
//
// Reproduces the bug the user reported: start at /components/, click
// the link to /components/confirmaction, click "Delete account" →
// dialog should open.
// =============================================================================

func TestE2E_SPA_PageScopedWidgetOpensAfterNav(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var catalogHas bool
	var widgetVisibleAfterClick bool
	err := chromedp.Run(ctx,
		// 1. Land on the index — no confirmation modal scoped here.
		chromedp.Navigate(base+"/components/"),
		pageReady(),
		// 2. Use the framework's client-side router by clicking the
		//    index link (NOT a fresh navigate). The runtime intercepts
		//    the <a> click and partial-fetches the new screen.
		chromedp.Evaluate(`document.querySelector('a[href="/components/confirmaction"]').click()`, nil),
		// Give the SPA swap + the catalog re-fetch a beat to land.
		chromedp.Sleep(700*1e6),
		// 3. The catalog must now have an entry for the page-scoped
		//    modal — that's the bug the fix targets.
		chromedp.Evaluate(`!!(window.__gofastr && window.__gofastr._widgetCatalog && window.__gofastr._widgetCatalog['demo-confirm-delete'])`, &catalogHas),
		// 4. Click the trigger and confirm the dialog actually opens.
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-confirm-delete"]').click()`, nil),
		chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-fui-widget="demo-confirm-delete"]');
			return !!el && !el.hasAttribute('hidden');
		})()`, &widgetVisibleAfterClick),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !catalogHas {
		t.Error("after SPA-nav, widget catalog must contain page-scoped 'demo-confirm-delete' entry — the runtime must re-fetch the catalog on gofastr:navigate")
	}
	if !widgetVisibleAfterClick {
		t.Error("clicking the trigger after SPA-nav must open the modal (regression test for the page-scoped widget bug)")
	}
}

func TestE2E_SPA_ColorSchemeBootstrapInjected(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var scheme string
	var bootstrapPresent bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/"),
		pageReady(),
		chromedp.Evaluate(`document.documentElement.getAttribute('data-color-scheme') || ''`, &scheme),
		chromedp.Evaluate(`!!document.querySelector('head script[src="/__gofastr/color-scheme.js"]')`, &bootstrapPresent),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !bootstrapPresent {
		t.Error("color-scheme bootstrap <script> must be injected into <head>")
	}
	if scheme != "light" && scheme != "dark" {
		t.Errorf("color-scheme bootstrap must set <html data-color-scheme>, got %q", scheme)
	}
}

func TestE2E_SPA_ColorSchemeRespondsToToggle(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var afterDark, afterLight, afterAuto string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/"),
		pageReady(),
		// Explicit dark
		chromedp.Evaluate(`window.__gofastr_colorScheme.set('dark')`, nil),
		settle(),
		chromedp.Evaluate(`document.documentElement.getAttribute('data-color-scheme')`, &afterDark),
		// Explicit light
		chromedp.Evaluate(`window.__gofastr_colorScheme.set('light')`, nil),
		settle(),
		chromedp.Evaluate(`document.documentElement.getAttribute('data-color-scheme')`, &afterLight),
		// Back to auto — follows OS pref (in headless, defaults to light).
		chromedp.Evaluate(`window.__gofastr_colorScheme.set('auto')`, nil),
		settle(),
		chromedp.Evaluate(`document.documentElement.getAttribute('data-color-scheme')`, &afterAuto),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if afterDark != "dark" {
		t.Errorf("set('dark') should set scheme to dark, got %q", afterDark)
	}
	if afterLight != "light" {
		t.Errorf("set('light') should set scheme to light, got %q", afterLight)
	}
	if afterAuto != "light" && afterAuto != "dark" {
		t.Errorf("set('auto') should fall back to OS preference (light or dark), got %q", afterAuto)
	}
}

func TestE2E_Regression_ModalBackdropClickCloses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var modalVisibleBeforeClick, modalVisibleAfterBackdropClick bool
	var backdropExists bool
	// Pick a point that's IN the dim area but OUTSIDE the modal card.
	// 12,12 is 12px from the viewport corner — well clear of the
	// centered modal content at 1280x800.
	var x, y float64 = 12, 12
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/confirmaction"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-confirm-delete"]').click()`, nil),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-fui-widget="demo-confirm-delete"]');
			return !!el && !el.hasAttribute('hidden');
		})()`, &modalVisibleBeforeClick),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-backdrop="demo-confirm-delete"]')`, &backdropExists),
		// REAL mouse click — must actually hit the backdrop element via
		// the browser's hit-testing, not bypass stacking like .click().
		// This is the regression: full-viewport .fui-pos-center sits on
		// top of the backdrop and absorbs every real click, even though
		// the backdrop has a listener attached.
		chromedp.MouseClickXY(x, y),
		chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-fui-widget="demo-confirm-delete"]');
			return !!el && !el.hasAttribute('hidden');
		})()`, &modalVisibleAfterBackdropClick),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !backdropExists {
		t.Fatal("backdrop element should exist when the modal is open")
	}
	if !modalVisibleBeforeClick {
		t.Fatal("modal should be visible after trigger click")
	}
	if modalVisibleAfterBackdropClick {
		t.Error("real backdrop click should dismiss the modal (regression — full-viewport .fui-pos-center wrapper was intercepting clicks)")
	}
}
