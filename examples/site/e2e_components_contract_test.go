package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Contract tests for each /components/* primitive — assert the
// behavioural baseline that other people build on. Failing any of
// these means the primitive has regressed and apps depending on it
// will silently break.
//
// Each test exercises one primitive end-to-end through a real
// headless browser: open, ARIA attrs, focus management, dismiss
// affordances. Keeps tight signal-to-noise — one assert per
// behaviour, no scaffolding noise.

// --- Modal ---------------------------------------------------------

func TestE2E_Modal_OpensWithCorrectARIA(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var role, ariaModal, labelledBy string
	var backdrop bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-modal"]').click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="site-demo-modal"]')?.getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="site-demo-modal"]')?.getAttribute('aria-modal')`, &ariaModal),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="site-demo-modal"]')?.getAttribute('aria-labelledby')`, &labelledBy),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-backdrop="site-demo-modal"]')`, &backdrop),
	); err != nil {
		t.Fatalf("modal: %v", err)
	}
	if role != "dialog" {
		t.Errorf("role = %q, want dialog", role)
	}
	if ariaModal != "true" {
		t.Errorf("aria-modal = %q, want true", ariaModal)
	}
	if labelledBy != "site-demo-modal-heading" {
		t.Errorf("aria-labelledby = %q, want site-demo-modal-heading", labelledBy)
	}
	if !backdrop {
		t.Error("expected backdrop element to be present")
	}
}

func TestE2E_Modal_EscClosesAndReturnsFocus(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var dismissed bool
	var bodyOverflow string
	var focusReturned bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-modal"]').focus()`, nil),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-modal"]').click()`, nil),
		// Lazy-fetched widget needs time for the chrome request + mount.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))`, nil),
		chromedp.Sleep(200*time.Millisecond),
		// Lazy-fetched (non-hydrated) widgets are detached on close;
		// SSR-inlined (hydrated) widgets are hidden in place. Either
		// way the widget should NOT be visible after Esc.
		chromedp.Evaluate(`(() => {
            const el = document.querySelector('[data-fui-widget="site-demo-modal"]');
            return !el || el.hasAttribute('hidden') || getComputedStyle(el).display === 'none';
        })()`, &dismissed),
		chromedp.Evaluate(`document.documentElement.style.overflow`, &bodyOverflow),
		chromedp.Evaluate(`document.activeElement === document.querySelector('button[data-fui-open="site-demo-modal"]')`, &focusReturned),
	); err != nil {
		t.Fatalf("modal Esc: %v", err)
	}
	if !dismissed {
		t.Error("modal should be dismissed after Esc (detached if lazy-fetched, hidden if SSR-inlined)")
	}
	if bodyOverflow != "" {
		t.Errorf("body overflow = %q, want '' (scroll restored)", bodyOverflow)
	}
	if !focusReturned {
		t.Error("focus should return to opener button on close")
	}
}

func TestE2E_Modal_DeepLinkOpensFromURL(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var heading string
	if err := chromedp.Run(ctx,
		// site-demo-modal deep-link: ?modal=user-edit&user_id=42
		chromedp.Navigate(base+"/components/modal?modal=user-edit&user_id=42"),
		pageReady(),
		chromedp.Sleep(500*time.Millisecond), // boot-time deep-link sync
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="site-demo-modal"] h3')?.textContent`, &heading),
	); err != nil {
		t.Fatalf("modal deeplink: %v", err)
	}
	// The site modal heading is "Edit user" (static; user_id is a deep-link param
	// that seeds a signal but the body text does not interpolate it).
	if !strings.Contains(heading, "Edit user") {
		t.Errorf("deeplinked modal heading = %q, want to contain 'Edit user'", heading)
	}
}

// --- Drawer --------------------------------------------------------

func TestE2E_Drawer_OpensWithBackdropAndScrollLock(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var role, ariaModal string
	var backdrop bool
	var overflow string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/drawer"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="site-demo-drawer"]').click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="site-demo-drawer"]')?.getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="site-demo-drawer"]')?.getAttribute('aria-modal')`, &ariaModal),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-backdrop="site-demo-drawer"]')`, &backdrop),
		chromedp.Evaluate(`document.documentElement.style.overflow`, &overflow),
	); err != nil {
		t.Fatalf("drawer: %v", err)
	}
	if role != "dialog" {
		t.Errorf("drawer role = %q", role)
	}
	if ariaModal != "true" {
		t.Errorf("drawer aria-modal = %q", ariaModal)
	}
	if !backdrop {
		t.Error("drawer should have a backdrop")
	}
	if overflow != "hidden" {
		t.Errorf("drawer should lock body scroll; got overflow=%q", overflow)
	}
}

// --- Toast ---------------------------------------------------------

func TestE2E_Toast_ServerHeaderFiresToast(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var titles string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toast"),
		pageReady(),
		// The server-toast button on site is labelled "Server: header".
		chromedp.Evaluate(`Array.from(document.querySelectorAll('button')).find(b => b.textContent.includes('Server: header')).click()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-notification__title')).map(n => n.textContent).join(',')`, &titles),
	); err != nil {
		t.Fatalf("toast server: %v", err)
	}
	if !strings.Contains(titles, "Saved") {
		t.Errorf("expected server toast 'Saved' in titles=%q", titles)
	}
}

func TestE2E_Toast_ClientJSAPIFiresToast(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var title, role, live string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toast"),
		pageReady(),
		chromedp.Evaluate(`window.__gofastr.toast({variant: 'danger', title: 'Test alert', body: 'body'})`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.ui-notification__title')?.textContent`, &title),
		chromedp.Evaluate(`document.querySelector('.ui-notification')?.getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('.ui-notification')?.getAttribute('aria-live')`, &live),
	); err != nil {
		t.Fatalf("toast client: %v", err)
	}
	if title != "Test alert" {
		t.Errorf("client toast title = %q", title)
	}
	if role != "alert" {
		t.Errorf("danger variant should use role=alert; got %q", role)
	}
	if live != "assertive" {
		t.Errorf("danger variant should use aria-live=assertive; got %q", live)
	}
}

// Note: burst-of-3 test dropped — the site's /components/toast page has
// client + server toast buttons but no "burst of 3" multi-toast trigger.

// --- Menu ----------------------------------------------------------

func TestE2E_Menu_RolesAndKeyboardNav(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var triggerHasPopup, panelRole, afterArrowDown, afterEnd, afterTypeS string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/menu"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('summary.ui-menu__trigger')?.getAttribute('aria-haspopup')`, &triggerHasPopup),
		chromedp.Evaluate(`document.querySelector('summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[role="menu"]')?.getAttribute('role')`, &panelRole),
		// Keyboard nav: dispatch on the focused item so e.target.closest works.
		// Site menu items: Profile → Settings → Sign out
		chromedp.Evaluate(`(() => {
            const items = document.querySelectorAll('[role="menuitem"]');
            items[0].focus();
            items[0].dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowDown', bubbles:true, cancelable:true}));
            return document.activeElement?.textContent.trim();
        })()`, &afterArrowDown),
		chromedp.Evaluate(`(() => {
            document.activeElement.dispatchEvent(new KeyboardEvent('keydown', {key:'End', bubbles:true, cancelable:true}));
            return document.activeElement?.textContent.trim();
        })()`, &afterEnd),
		// Type-ahead 's' from first item should jump to Settings.
		chromedp.Evaluate(`(() => {
            const items = document.querySelectorAll('[role="menuitem"]');
            items[0].focus();
            items[0].dispatchEvent(new KeyboardEvent('keydown', {key:'s', bubbles:true, cancelable:true}));
            return document.activeElement?.textContent.trim();
        })()`, &afterTypeS),
	); err != nil {
		t.Fatalf("menu: %v", err)
	}
	if triggerHasPopup != "menu" {
		t.Errorf("aria-haspopup = %q", triggerHasPopup)
	}
	if panelRole != "menu" {
		t.Errorf("panel role = %q", panelRole)
	}
	if afterArrowDown != "Settings" {
		t.Errorf("ArrowDown from Profile should reach Settings, got %q", afterArrowDown)
	}
	if afterEnd != "Sign out" {
		t.Errorf("End should jump to last item Sign out, got %q", afterEnd)
	}
	if afterTypeS != "Settings" {
		t.Errorf("Type-ahead 's' should jump to Settings, got %q", afterTypeS)
	}
}

// --- Sidebar (mobile nav drawer) -----------------------------------
//
// The /components/sidebar showcase renders a ui.Sidebar whose hamburger
// (< 900px) opens the ui-sidebar-drawer widget. That drawer must be mounted
// for the hamburger to do anything — historically it was not, so the button
// silently no-opened. This is the contract test for "the hamburger opens the
// drawer".

func TestE2E_Sidebar_HamburgerOpensDrawer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var triggerExists, drawerPresent bool
	var role, ariaModal string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sidebar"),
		pageReady(),
		chromedp.Evaluate(`!!document.querySelector('button.ui-sidebar__hamburger[data-fui-open="ui-sidebar-drawer"]')`, &triggerExists),
		// Click via JS so the test is viewport-independent — the open is gated
		// on the runtime handler, not CSS visibility.
		chromedp.Evaluate(`document.querySelector('button.ui-sidebar__hamburger[data-fui-open="ui-sidebar-drawer"]')?.click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-widget="ui-sidebar-drawer"]')`, &drawerPresent),
		// Coalesce undefined→'' so an absent drawer fails on the assertion below
		// rather than a chromedp "undefined value" error.
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="ui-sidebar-drawer"]')?.getAttribute('role') ?? ''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="ui-sidebar-drawer"]')?.getAttribute('aria-modal') ?? ''`, &ariaModal),
	); err != nil {
		t.Fatalf("sidebar hamburger: %v", err)
	}
	if !triggerExists {
		t.Fatal("hamburger trigger button[data-fui-open=ui-sidebar-drawer] missing from the showcase")
	}
	if !drawerPresent {
		t.Fatal("hamburger click did not open the sidebar drawer — ui-sidebar-drawer is not mounted")
	}
	if role != "dialog" {
		t.Errorf("sidebar drawer role = %q, want dialog", role)
	}
	if ariaModal != "true" {
		t.Errorf("sidebar drawer aria-modal = %q, want true", ariaModal)
	}
}

func TestE2E_SidebarVariantsAdaptAndPersist(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	const storageKey = "gofastr.sidebar.ui-sidebar-drawer.collapsed"
	var collapsed, persisted, labelHidden, offCanvasHidden, hamburgerVisible bool
	var offCanvasState []bool
	var expanded string
	var inlineWidth float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sidebar"),
		pageReady(),
		chromedp.Evaluate(`localStorage.removeItem("`+storageKey+`")`, nil),
		chromedp.Reload(),
		pageReady(),
		chromedp.WaitVisible(`[data-fui-sidebar-collapse]`, chromedp.ByQuery),
		chromedp.Click(`[data-fui-sidebar-collapse]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-fui-sidebar]')?.dataset.collapsed === 'true'`, &collapsed),
		chromedp.Evaluate(`document.querySelector('[data-fui-sidebar-collapse]')?.getAttribute('aria-expanded') ?? ''`, &expanded),
		// Labels are clipped (visually-hidden pattern), NOT display:none —
		// focusable links must keep their accessible names when collapsed.
		chromedp.Evaluate(`(() => {
			const l = document.querySelector('[data-fui-sidebar] .ui-sidebar__label');
			const cs = getComputedStyle(l);
			return cs.position === 'absolute' && l.getBoundingClientRect().width <= 1 && cs.display !== 'none';
		})()`, &labelHidden),
		chromedp.Evaluate(`document.querySelector('[data-fui-sidebar] .ui-sidebar__inline').getBoundingClientRect().width`, &inlineWidth),
		chromedp.Reload(),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-sidebar]')?.dataset.collapsed === 'true'`, &persisted),
		chromedp.Evaluate(`
			const sidebar = document.querySelector('[data-fui-sidebar]');
			sidebar.classList.remove('ui-sidebar--collapsible');
			sidebar.classList.add('ui-sidebar--off-canvas');
			[
				getComputedStyle(sidebar.querySelector('.ui-sidebar__inline')).display === 'none',
				getComputedStyle(sidebar.querySelector('.ui-sidebar__hamburger')).display !== 'none'
			]
		`, &offCanvasState),
	); err != nil {
		t.Fatalf("sidebar variants: %v", err)
	}
	if len(offCanvasState) == 2 {
		offCanvasHidden, hamburgerVisible = offCanvasState[0], offCanvasState[1]
	}
	if !collapsed || expanded != "false" || !labelHidden || inlineWidth > 80 {
		t.Errorf("collapsed state incomplete: collapsed=%v expanded=%q labelHidden=%v width=%.1f", collapsed, expanded, labelHidden, inlineWidth)
	}
	if !persisted {
		t.Error("collapsed state did not survive reload")
	}
	if !offCanvasHidden || !hamburgerVisible {
		t.Errorf("off-canvas desktop state incorrect: inlineHidden=%v hamburgerVisible=%v", offCanvasHidden, hamburgerVisible)
	}
}

// Note: Popover tests dropped — the site has no /components/popover page.
