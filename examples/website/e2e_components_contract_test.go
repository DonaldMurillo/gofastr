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
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var role, ariaModal, labelledBy string
	var backdrop bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="components-confirm"]').click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-confirm"]')?.getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-confirm"]')?.getAttribute('aria-modal')`, &ariaModal),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-confirm"]')?.getAttribute('aria-labelledby')`, &labelledBy),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-backdrop="components-confirm"]')`, &backdrop),
	); err != nil {
		t.Fatalf("modal: %v", err)
	}
	if role != "dialog" {
		t.Errorf("role = %q, want dialog", role)
	}
	if ariaModal != "true" {
		t.Errorf("aria-modal = %q, want true", ariaModal)
	}
	if labelledBy != "components-confirm-title" {
		t.Errorf("aria-labelledby = %q", labelledBy)
	}
	if !backdrop {
		t.Error("expected backdrop element to be present")
	}
}

func TestE2E_Modal_EscClosesAndReturnsFocus(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var dismissed bool
	var bodyOverflow string
	var focusReturned bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="components-confirm"]').focus()`, nil),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="components-confirm"]').click()`, nil),
		// Lazy-fetched widget needs time for the chrome request + mount.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))`, nil),
		chromedp.Sleep(200*time.Millisecond),
		// Lazy-fetched (non-hydrated) widgets are detached on close;
		// SSR-inlined (hydrated) widgets are hidden in place. Either
		// way the widget should NOT be visible after Esc.
		chromedp.Evaluate(`(() => {
            const el = document.querySelector('[data-fui-widget="components-confirm"]');
            return !el || el.hasAttribute('hidden') || getComputedStyle(el).display === 'none';
        })()`, &dismissed),
		chromedp.Evaluate(`document.body.style.overflow`, &bodyOverflow),
		chromedp.Evaluate(`document.activeElement === document.querySelector('button[data-fui-open="components-confirm"]')`, &focusReturned),
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
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var heading string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/modal?modal=user-edit&user_id=42"),
		pageReady(),
		chromedp.Sleep(500*time.Millisecond), // boot-time deep-link sync
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-user-edit"] h2')?.textContent`, &heading),
	); err != nil {
		t.Fatalf("modal deeplink: %v", err)
	}
	if !strings.Contains(heading, "Edit user 42") {
		t.Errorf("deeplinked modal heading = %q, want 'Edit user 42' (signal seeded)", heading)
	}
}

// --- Drawer --------------------------------------------------------

func TestE2E_Drawer_OpensWithBackdropAndScrollLock(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var role, ariaModal string
	var backdrop bool
	var overflow string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/drawer"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="components-drawer"]').click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-drawer"]')?.getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-drawer"]')?.getAttribute('aria-modal')`, &ariaModal),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-backdrop="components-drawer"]')`, &backdrop),
		chromedp.Evaluate(`document.body.style.overflow`, &overflow),
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
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var titles string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toast"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('button')).find(b => b.textContent.includes('Server: success')).click()`, nil),
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

func TestE2E_Toast_BurstFiresMultipleFromOneResponse(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var count int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toast"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('button')).find(b => b.textContent.includes('burst of 3')).click()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('.ui-toast-stack__item').length`, &count),
	); err != nil {
		t.Fatalf("toast burst: %v", err)
	}
	if count < 3 {
		t.Errorf("burst expected at least 3 toasts, got %d", count)
	}
}

// --- Menu ----------------------------------------------------------

func TestE2E_Menu_RolesAndKeyboardNav(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var triggerHasPopup, panelRole, afterArrowDown, afterEnd, afterTypeA string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/menu"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('summary.ui-menu__trigger')?.getAttribute('aria-haspopup')`, &triggerHasPopup),
		chromedp.Evaluate(`document.querySelector('summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[role="menu"]')?.getAttribute('role')`, &panelRole),
		// Keyboard nav: dispatch on the focused item so e.target.closest works.
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
		chromedp.Evaluate(`(() => {
            const items = document.querySelectorAll('[role="menuitem"]');
            items[0].focus();
            items[0].dispatchEvent(new KeyboardEvent('keydown', {key:'a', bubbles:true, cancelable:true}));
            return document.activeElement?.textContent.trim();
        })()`, &afterTypeA),
	); err != nil {
		t.Fatalf("menu: %v", err)
	}
	if triggerHasPopup != "menu" {
		t.Errorf("aria-haspopup = %q", triggerHasPopup)
	}
	if panelRole != "menu" {
		t.Errorf("panel role = %q", panelRole)
	}
	if afterArrowDown != "Duplicate" {
		t.Errorf("ArrowDown from Edit should reach Duplicate, got %q", afterArrowDown)
	}
	if afterEnd != "Delete" {
		t.Errorf("End should jump to last item Delete, got %q", afterEnd)
	}
	if afterTypeA != "Archive" {
		t.Errorf("Type-ahead 'a' should jump to Archive, got %q", afterTypeA)
	}
}

// --- Sidebar -------------------------------------------------------

func TestE2E_Sidebar_ActiveItemHasAriaCurrent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var activeLabel string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sidebar"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-sidebar] [aria-current="page"]')?.textContent.trim() || ''`, &activeLabel),
	); err != nil {
		t.Fatalf("sidebar active: %v", err)
	}
	if !strings.Contains(activeLabel, "Components") {
		t.Errorf("active sidebar item should be Components (matchPath=/components); got %q", activeLabel)
	}
}

func TestE2E_Sidebar_HamburgerOpensDrawerOnNarrow(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hambDisplay, inlineDisplay string
	var drawerOpen bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 700),
		chromedp.Navigate(base+"/components/sidebar"),
		pageReady(),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-sidebar__hamburger')).display`, &hambDisplay),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-sidebar__inline')).display`, &inlineDisplay),
		chromedp.Evaluate(`document.querySelector('.ui-sidebar__hamburger').click()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-widget="ui-sidebar-drawer"]')`, &drawerOpen),
	); err != nil {
		t.Fatalf("sidebar hamburger: %v", err)
	}
	if hambDisplay == "none" {
		t.Errorf("hamburger should be visible on narrow viewport; display=%q", hambDisplay)
	}
	if inlineDisplay != "none" {
		t.Errorf("inline column should be hidden on narrow viewport; display=%q", inlineDisplay)
	}
	if !drawerOpen {
		t.Error("hamburger click should mount the sidebar-drawer widget")
	}
}
