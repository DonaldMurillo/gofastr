package main

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// The shell's two no-JavaScript properties, on a phone: the nav opens
// through the native <details> menu below md, and a direct load of an
// issue page shows the issue with a real back-to-list link. Script
// execution is disabled for the whole browser context BEFORE the first
// navigation, so runtime.js (and the app's own hud.js) never run —
// what passes below is the platform and the server's HTML alone.

// nojsPhone opens a 390×844 browser with script execution disabled.
func nojsPhone(t *testing.T) context.Context {
	t.Helper()
	root := trackerBrowser(t, 390, 844)
	if err := chromedp.Run(root, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable script execution: %v", err)
	}
	return root
}

// nojsLanded polls until the browser sits at path and main's text
// contains want. With script off every link is a full page load, and a
// full navigation tears up the execution context mid-poll (-32000), so
// the poll is a loop of FRESH evaluates — each chromedp.Run binds to
// whichever context is live now, and the ones racing the navigation
// are simply retried.
func nojsLanded(t *testing.T, ctx context.Context, path, want string) {
	t.Helper()
	expr := `location.pathname === ` + jsString(path) +
		` && (document.querySelector('main') || {}).textContent.includes(` + jsString(want) + `)`
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err == nil && ok {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("never landed on %s ~ %q", path, want)
}

// jsString renders s as a double-quoted JavaScript string literal.
func jsString(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			out = append(out, '\\', c)
		default:
			out = append(out, c)
		}
	}
	return string(append(out, '"'))
}

// TestTrackerPhoneNavWithoutJS: below md, with no script, the sidebar's
// nav is reachable through the native <details> menu and following one
// of its links lands on the page. Mutation it catches: deleting the
// menu from the shell (or hiding it below md) leaves the phone with
// the inert widget-drawer trigger and no reachable nav — the wait for
// the open panel's link fails.
func TestTrackerPhoneNavWithoutJS(t *testing.T) {
	base := trackerServe(t)
	root := nojsPhone(t)
	ctx, cancel := trackerTab(t, root, 45*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/settings"),
		chromedp.WaitReady("main", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("load /settings: %v", err)
	}

	// The native disclosure replaces the drawer trigger without scripting.
	var (
		startsClosed bool
		triggerShown string
	)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-sidebar-native__mobile > details').open`, &startsClosed),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-sidebar__hamburger')).display`, &triggerShown),
	); err != nil {
		t.Fatalf("probe the phone header: %v", err)
	}
	if startsClosed {
		t.Error("the native phone menu starts open; it should be closed until activated")
	}
	if triggerShown != "none" {
		t.Errorf("the widget drawer's trigger is visible (%q) without script; only the native menu should show", triggerShown)
	}

	// Open the menu with a real click (the platform toggles <details>),
	// then follow the Reports link.
	if err := chromedp.Run(ctx,
		chromedp.Click(".fui-sidebar-native__mobile > details > summary", chromedp.NodeVisible),
		chromedp.WaitVisible(`.fui-sidebar-native__mobile .fui-collapsible__content a[href="/reports"]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('.fui-sidebar-native__mobile > details').open`, &startsClosed),
	); err != nil {
		t.Fatalf("open the native menu: %v", err)
	}
	if !startsClosed {
		t.Error("clicking the summary did not open the native menu")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`.fui-sidebar-native__mobile .fui-collapsible__content a[href="/reports"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatalf("follow the menu's Reports link: %v", err)
	}

	// A full page load lands on Reports with the nav reachable again.
	nojsLanded(t, ctx, "/reports", "Reports")
	var menuBack bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`!!document.querySelector('.fui-sidebar-native__mobile > details > summary') &&
			getComputedStyle(document.querySelector('.fui-sidebar-native__mobile > details')).display !== 'none'`, &menuBack),
	); err != nil {
		t.Fatalf("probe the landed page's menu: %v", err)
	}
	if !menuBack {
		t.Error("the native menu is not reachable on the page the link landed on")
	}
}

// TestTrackerDirectIssueWithoutJS: a direct load of an issue page on a
// phone shows the issue, the list pane hides, and the back-to-list link
// is a real visible anchor to the project — following it lands on the
// project. Mutation it catches: dropping the back link (or hiding it on
// the phone) leaves a one-pane issue page with no way back.
func TestTrackerDirectIssueWithoutJS(t *testing.T) {
	base := trackerServe(t)
	root := nojsPhone(t)
	ctx, cancel := trackerTab(t, root, 45*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/42"),
		chromedp.WaitReady("#issue-detail", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("direct load of an issue: %v", err)
	}

	var (
		backShown string
		backHref  string
		listShown string
	)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-list-detail__detail a.fui-button')).display`, &backShown),
		chromedp.Evaluate(`document.querySelector('.fui-list-detail__detail a.fui-button').getAttribute('href')`, &backHref),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-list-detail__list')).display`, &listShown),
	); err != nil {
		t.Fatalf("probe the issue page: %v", err)
	}
	if backShown == "none" {
		t.Error("the back-to-list link is hidden on the phone")
	}
	if backHref != "/projects/billing" {
		t.Errorf("back link href = %q, want /projects/billing", backHref)
	}
	if listShown != "none" {
		t.Error("the issue list must hide when an issue is open on a phone")
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(".fui-list-detail__detail a.fui-button", chromedp.NodeVisible),
	); err != nil {
		t.Fatalf("follow the back link: %v", err)
	}
	nojsLanded(t, ctx, "/projects/billing", "Billing")
}
