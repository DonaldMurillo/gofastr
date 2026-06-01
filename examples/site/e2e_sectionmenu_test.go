package main

// Browser-level e2e for the interactive SectionMenu — the unified docs +
// components navigation. The mobile sheet is the framework's preset.Drawer
// widget, so these assert the real out-of-the-box behaviours the user asked
// for: close-on-outside-click, scroll-lock, and NO layout shift on open.

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// Desktop: the menu is a rail (trigger hidden, groups force-expanded) and the
// current item is highlighted with the primary-coloured left border.
func TestE2E_SectionMenu_DesktopRail(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t) // 1280×800 → ≥900px → rail mode

	var triggerDisplay, railDisplay, activeBorder string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/docs/entity-declarations"),
		chromedp.WaitReady(`[data-fui-comp="fui-section-menu"]`, chromedp.ByQuery),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-section-menu__trigger')).display`, &triggerDisplay),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-section-menu__rail')).display`, &railDisplay),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-section-menu__link.is-active')).borderLeftColor`, &activeBorder),
	); err != nil {
		t.Fatalf("desktop rail: %v", err)
	}
	if triggerDisplay != "none" {
		t.Errorf("desktop: the mobile trigger must be hidden, got display=%q", triggerDisplay)
	}
	if railDisplay == "none" {
		t.Errorf("desktop: the rail must be visible, got display=%q", railDisplay)
	}
	if activeBorder == "" || strings.Contains(activeBorder, ", 0)") || activeBorder == "rgba(0, 0, 0, 0)" {
		t.Errorf("active item should have a coloured left border, got %q", activeBorder)
	}
}

// Mobile: the trigger opens the framework drawer. Asserts no layout shift, a
// scroll-locked backdrop, and close-on-outside-click (the two reported bugs).
func TestE2E_SectionMenu_MobileDrawer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	const drawer = "components-section-menu"
	var viewportW, triggerTopBefore, triggerTopAfter float64
	var overflowClosed, overflowOpen, overflowAfterBackdrop string
	var backdropPresent bool

	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, true),
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady(`.fui-section-menu__trigger`, chromedp.ByQuery),
		chromedp.Evaluate(`window.innerWidth`, &viewportW),
		chromedp.Evaluate(`getComputedStyle(document.body).overflow`, &overflowClosed),
		chromedp.Evaluate(`document.querySelector('.fui-section-menu__trigger').getBoundingClientRect().top`, &triggerTopBefore),
		// Open the drawer.
		chromedp.Click(`.fui-section-menu__trigger`, chromedp.ByQuery),
		chromedp.Sleep(700*time.Millisecond), // lazy widget chrome + open
		chromedp.Evaluate(`document.querySelector('.fui-section-menu__trigger').getBoundingClientRect().top`, &triggerTopAfter),
		chromedp.Evaluate(`getComputedStyle(document.body).overflow`, &overflowOpen),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-backdrop="`+drawer+`"]')`, &backdropPresent),
		// Close on OUTSIDE click — tap the exposed backdrop strip to the right
		// of the ~337px (90vw) drawer panel. Clicking the panel-covered centre
		// would (correctly) NOT dismiss, so target the dim area at x≈365.
		chromedp.MouseClickXY(365, 400),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.body).overflow`, &overflowAfterBackdrop),
	); err != nil {
		t.Fatalf("mobile drawer: %v", err)
	}

	if viewportW > 900 {
		t.Fatalf("viewport emulation failed: innerWidth=%.0f", viewportW)
	}
	// Bug #2: the trigger must NOT move when the drawer opens (no layout shift).
	if d := triggerTopAfter - triggerTopBefore; d > 1 || d < -1 {
		t.Errorf("opening the drawer shifted the trigger by %.1fpx (layout shift)", d)
	}
	if overflowClosed == "hidden" {
		t.Errorf("background scroll should not be locked before opening, got %q", overflowClosed)
	}
	if overflowOpen != "hidden" {
		t.Errorf("opening the drawer should lock background scroll, got overflow=%q", overflowOpen)
	}
	if !backdropPresent {
		t.Error("drawer should render a backdrop")
	}
	// Bug #1: tapping the backdrop (outside) must close the drawer.
	if overflowAfterBackdrop == "hidden" {
		t.Errorf("clicking the backdrop must close the drawer (scroll-lock released); overflow still %q", overflowAfterBackdrop)
	}
}

// Mobile: a visible × button closes the drawer, and closing preserves the
// page's scroll position (no jump — the two reported bugs).
func TestE2E_SectionMenu_CloseButtonAndScrollPreserved(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const drawer = "components-section-menu"

	var closeVisible bool
	var scrollBefore, scrollAfterOpen, scrollAfterClose float64
	var overflowAfterClose string
	// Use JS .click() (not chromedp.Click, which auto-scrolls the target into
	// view) so we measure the widget's own scroll behaviour, not the harness's.
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, true),
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady(`.fui-section-menu__trigger`, chromedp.ByQuery),
		// Scroll the page down so an open/close scroll jump would be visible.
		chromedp.Evaluate(`window.scrollTo(0, 600)`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &scrollBefore),
		chromedp.Evaluate(`document.querySelector('.fui-section-menu__trigger').click()`, nil),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &scrollAfterOpen),
		// The × close button must be visible inside the drawer.
		chromedp.Evaluate(`(()=>{const b=document.querySelector('[data-fui-widget="`+drawer+`"] .fui-section-menu__close');return !!b && b.getBoundingClientRect().width>0})()`, &closeVisible),
		// Close via the × button (data-fui-action="close").
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="`+drawer+`"] .fui-section-menu__close').click()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &scrollAfterClose),
		chromedp.Evaluate(`getComputedStyle(document.body).overflow`, &overflowAfterClose),
	); err != nil {
		t.Fatalf("close button: %v", err)
	}
	if !closeVisible {
		t.Error("the drawer must show a visible × close button")
	}
	if overflowAfterClose == "hidden" {
		t.Errorf("the × button must close the drawer (scroll-lock released); overflow=%q", overflowAfterClose)
	}
	// Opening and closing must not move the page.
	if d := scrollAfterOpen - scrollBefore; d > 1 || d < -1 {
		t.Errorf("opening the drawer scrolled the page by %.0fpx", d)
	}
	if d := scrollAfterClose - scrollBefore; d > 1 || d < -1 {
		t.Errorf("closing the drawer scrolled the page by %.0fpx (the reported bug)", d)
	}
}

// Mobile: picking a link inside the drawer navigates AND auto-closes it.
func TestE2E_SectionMenu_DrawerClosesOnNav(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const drawer = "components-section-menu"

	var pathBefore, pathAfter, overflowAfter string
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, true),
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady(`.fui-section-menu__trigger`, chromedp.ByQuery),
		chromedp.Click(`.fui-section-menu__trigger`, chromedp.ByQuery),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &pathBefore),
		// Tap the "Overview" lead link inside the open drawer.
		chromedp.Click(`[data-fui-widget="`+drawer+`"] .fui-section-menu__lead`, chromedp.ByQuery),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &pathAfter),
		chromedp.Evaluate(`getComputedStyle(document.body).overflow`, &overflowAfter),
	); err != nil {
		t.Fatalf("drawer nav: %v", err)
	}
	if pathAfter == pathBefore {
		t.Errorf("tapping a drawer link should navigate (before=%s after=%s)", pathBefore, pathAfter)
	}
	if overflowAfter == "hidden" {
		t.Errorf("the drawer must auto-close after navigation (scroll-lock released); overflow=%q", overflowAfter)
	}
}
