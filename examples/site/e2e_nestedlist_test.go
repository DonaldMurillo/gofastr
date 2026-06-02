package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// E2E tests for /components/nestedlist ported from examples/website.
//
// Site renders a "Settings" nested list:
//   Account (expanded) → Profile, Security
//   Notifications (collapsed) → Email, Push
//   Billing (leaf link)
//
// The original website tests used a different label ("Project team") and
// different leaf items; selectors are updated here to match site's demo.
// =============================================================================

func TestE2ENestedListFlat(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var ulCount, linkCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('ul.nested-list').length`, &ulCount),
		chromedp.Evaluate(`document.querySelectorAll('.nested-list__link').length`, &linkCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ulCount < 1 {
		t.Error("expected at least one ul.nested-list")
	}
	// Account has 2 leaf links + Billing leaf = 3 initially visible
	if linkCount < 3 {
		t.Errorf("expected ≥3 nested-list links, got %d", linkCount)
	}
}

func TestE2ENestedListBranchDetails(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var detailsCount, openCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.nested-list details.nested-list__branch').length`, &detailsCount),
		// "Account" branch is initially expanded (Expanded: true in config).
		chromedp.Evaluate(`document.querySelectorAll('.nested-list details[open]').length`, &openCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if detailsCount < 2 {
		t.Errorf("expected ≥2 <details> branches on the nested demo (Account + Notifications), got %d", detailsCount)
	}
	if openCount < 1 {
		t.Errorf("expected at least one initially-expanded branch (Account), got %d", openCount)
	}
}

// TestE2ENestedListStyled asserts that BaseCSS() for the nestedlist pattern
// was wired into the site theme — same regression guard as in the website
// suite, pinning the "shipped without its stylesheet" failure mode.
func TestE2ENestedListStyled(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var liStyle, summaryStyle string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		// Outer <ul> reset: padding-inline-start must be 0 (not browser default ~40px).
		chromedp.Evaluate(`(function(){
			var ul = document.querySelector('ul.nested-list');
			return ul ? getComputedStyle(ul).paddingInlineStart : '';
		})()`, &liStyle),
		// Branch summary should be flex (our custom layout).
		chromedp.Evaluate(`(function(){
			var s = document.querySelector('.nested-list summary');
			return s ? getComputedStyle(s).display : '';
		})()`, &summaryStyle),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if liStyle != "0px" {
		t.Errorf("expected ul.nested-list padding-inline-start: 0 (BaseCSS reset), got %q", liStyle)
	}
	if !strings.Contains(summaryStyle, "flex") {
		t.Errorf("expected summary display: inline-flex (our BaseCSS), got %q", summaryStyle)
	}
}

// TestE2ENestedListAccountExpanded specifically checks that the "Account"
// branch is pre-expanded with Profile and Security visible.
func TestE2ENestedListAccountExpanded(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var accountOpen bool
	var childLinks int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		// Account summary text lives inside the <details> that's [open].
		chromedp.Evaluate(`(function(){
			var branches = document.querySelectorAll('.nested-list details.nested-list__branch[open]');
			for (var d of branches) {
				if (d.querySelector('summary')?.textContent.trim() === 'Account') return true;
			}
			return false;
		})()`, &accountOpen),
		// Profile + Security are direct children of the Account branch.
		chromedp.Evaluate(`(function(){
			var branches = document.querySelectorAll('.nested-list details.nested-list__branch[open]');
			for (var d of branches) {
				if (d.querySelector('summary')?.textContent.trim() === 'Account') {
					return d.querySelectorAll('.nested-list__link').length;
				}
			}
			return 0;
		})()`, &childLinks),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !accountOpen {
		t.Error("expected Account branch to be expanded by default")
	}
	if childLinks < 2 {
		t.Errorf("expected ≥2 leaf links under Account (Profile + Security), got %d", childLinks)
	}
}

// TestE2ENestedListToggleBranch clicks the Notifications summary (collapsed
// by default) and asserts it opens, then closes again.
func TestE2ENestedListToggleBranch(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var openBefore, openAfter, openAfterClose bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		// Find the Notifications <details> (starts closed).
		chromedp.Evaluate(`(function(){
			for (var d of document.querySelectorAll('.nested-list details.nested-list__branch')) {
				if (d.querySelector('summary')?.textContent.trim() === 'Notifications') return d.hasAttribute('open');
			}
			return null;
		})()`, &openBefore),
		// Click its summary to expand.
		chromedp.Evaluate(`(function(){
			for (var d of document.querySelectorAll('.nested-list details.nested-list__branch')) {
				if (d.querySelector('summary')?.textContent.trim() === 'Notifications') {
					d.querySelector('summary').click();
					return true;
				}
			}
			return false;
		})()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`(function(){
			for (var d of document.querySelectorAll('.nested-list details.nested-list__branch')) {
				if (d.querySelector('summary')?.textContent.trim() === 'Notifications') return d.hasAttribute('open');
			}
			return null;
		})()`, &openAfter),
		// Click again to close.
		chromedp.Evaluate(`(function(){
			for (var d of document.querySelectorAll('.nested-list details.nested-list__branch')) {
				if (d.querySelector('summary')?.textContent.trim() === 'Notifications') {
					d.querySelector('summary').click();
					return true;
				}
			}
			return false;
		})()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`(function(){
			for (var d of document.querySelectorAll('.nested-list details.nested-list__branch')) {
				if (d.querySelector('summary')?.textContent.trim() === 'Notifications') return d.hasAttribute('open');
			}
			return null;
		})()`, &openAfterClose),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openBefore {
		t.Error("Notifications should start collapsed")
	}
	if !openAfter {
		t.Error("Notifications should be expanded after first click")
	}
	if openAfterClose {
		t.Error("Notifications should be collapsed after second click")
	}
}
