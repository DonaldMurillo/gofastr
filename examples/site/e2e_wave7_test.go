package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// E2E tests ported from examples/website for Wave 7 components:
// Select, AspectRatio, BackToTop, SkipLink, ThemeToggle, Sticky.
//
// Dropped: toggle (website had a /components/toggle slug; site uses
// /components/switch for the ui.Switch component and the existing
// e2e_test.go already covers that via TestE2E_ToggleFlipsValue).
// RadioGroup/CheckboxGroup tests from website's e2e_wave7_test.go all
// hit /components/toggle — there is no equivalent slug in site that
// exposes .ui-toggle-group markup, so those are dropped.
// =============================================================================

// ─── Select ─────────────────────────────────────────────────────────

func TestE2E_Select_BasicRenders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	var hasLabel bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-select"]').length`, &count),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-select"] label.ui-select__label') !== null`, &hasLabel),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 1 {
		t.Error("expected at least one ui-select component on the page")
	}
	if !hasLabel {
		t.Error("expected a <label> with class ui-select__label inside the component")
	}
}

func TestE2E_Select_HasOptions(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var optCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-select"] select option').length`, &optCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if optCount < 3 {
		t.Errorf("expected ≥3 options in the select demo, got %d", optCount)
	}
}

func TestE2E_Select_CustomArrow(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var bgImage string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var sel = document.querySelector('[data-fui-comp="ui-select"] select');
			return sel ? getComputedStyle(sel).backgroundImage : '';
		})()`, &bgImage),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if bgImage == "" || bgImage == "none" {
		t.Error("expected custom chevron background-image on the <select>")
	}
}

// ─── AspectRatio ────────────────────────────────────────────────────

func TestE2E_AspectRatio_RendersBoxes(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/aspectratio"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-aspect-ratio"]').length`, &count),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 1 {
		t.Errorf("expected ≥1 aspect-ratio boxes on the page, got %d", count)
	}
}

func TestE2E_AspectRatio_CSSAppliesAspectRatio(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var ar string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/aspectratio"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var el = document.querySelector('[data-fui-comp="ui-aspect-ratio"]');
			return el ? getComputedStyle(el).aspectRatio : '';
		})()`, &ar),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ar == "" || ar == "auto" {
		t.Errorf("expected aspect-ratio CSS to apply, got %q", ar)
	}
}

// ─── SkipLink ───────────────────────────────────────────────────────

func TestE2E_SkipLink_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var href, text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skiplink"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-skip-link"]')?.getAttribute('href') || ''`, &href),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-skip-link"]')?.textContent || ''`, &text),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if href == "" {
		t.Errorf("expected href on skip link, got empty")
	}
	if !strings.Contains(text, "Skip") {
		t.Errorf("expected link text to contain 'Skip', got %q", text)
	}
}

func TestE2E_SkipLink_HiddenByDefault(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var offScreen bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skiplink"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var link = document.querySelector('a[data-fui-comp="ui-skip-link"]');
			if (!link) return false;
			var rect = link.getBoundingClientRect();
			return rect.left < -100 || (rect.width === 0 && rect.height === 0);
		})()`, &offScreen),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !offScreen {
		t.Error("SkipLink should be off-screen by default (only visible on focus)")
	}
}

// ─── ThemeToggle ────────────────────────────────────────────────────

func TestE2E_ThemeToggle_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var iconPresent bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/themetoggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-theme-toggle]') !== null`, &iconPresent),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !iconPresent {
		t.Error("expected at least one ThemeToggle button")
	}
}

func TestE2E_ThemeToggle_ClickCyclesScheme(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var schemeAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/themetoggle"),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('button[data-fui-theme-toggle]')?.click()`, nil),
		settle(),
		chromedp.Evaluate(`document.documentElement.getAttribute('data-color-scheme') || ''`, &schemeAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if schemeAfter == "" {
		t.Error("expected data-color-scheme to be set on <html> after clicking ThemeToggle")
	}
}

// ─── Sticky ─────────────────────────────────────────────────────────

func TestE2E_Sticky_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sticky"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-sticky"]').length`, &count),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 1 {
		t.Errorf("expected ≥1 sticky elements on the page, got %d", count)
	}
}

func TestE2E_Sticky_TopHasStickyCSS(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pos string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sticky"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var el = document.querySelector('[data-fui-comp="ui-sticky"].ui-sticky--top');
			return el ? getComputedStyle(el).position : 'none';
		})()`, &pos),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pos != "sticky" && pos != "-webkit-sticky" {
		t.Errorf("expected position:sticky, got %q", pos)
	}
}

// ─── BackToTop ──────────────────────────────────────────────────────

func TestE2E_BackToTop_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var exists bool
	var tag, ariaLabel string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]') !== null`, &exists),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.tagName?.toLowerCase() || ''`, &tag),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.getAttribute('aria-label') || ''`, &ariaLabel),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected [data-fui-back-to-top] element to exist")
	}
	if tag != "button" {
		t.Errorf("expected tag button, got %q", tag)
	}
	if ariaLabel == "" {
		t.Errorf("expected aria-label on BackToTop, got empty")
	}
}

func TestE2E_BackToTop_HiddenByDefault(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var inert bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('inert') || false`, &inert),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !inert {
		t.Error("expected inert attribute initially (button must not be focusable when hidden)")
	}
}

func TestE2E_BackToTop_RuntimeModuleLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var loaded bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.backtotop) || false`, &loaded),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.Error("expected backtotop runtime module to be loaded")
	}
}

func TestE2E_BackToTop_ScrollShowsButton(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var visible, inert bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`window.scrollTo(0, 600)`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('data-fui-btt-visible') || false`, &visible),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('inert') || false`, &inert),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !visible {
		t.Error("expected data-fui-btt-visible after scrolling past threshold")
	}
	if inert {
		t.Error("expected inert to be removed after scrolling, button should be focusable")
	}
}

func TestE2E_BackToTop_ClickScrollsToTop(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var scrollY int64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`window.scrollTo(0, 800)`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top][data-fui-btt-visible]')?.click()`, nil),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &scrollY),
	)
	if err != nil {
		t.Fatal(err)
	}
	if scrollY > 50 {
		t.Errorf("expected scrollY near 0 after clicking BackToTop, got %d", scrollY)
	}
}
