package main

// TestSidebarKeepsScrollAndOpenGroup (docs/DESIGN-layout-outlets.md
// "Reactive areas", design L727-734): a sidebar is a STATIC area of a
// kept layer. Two guarantees against the real tracker app:
//
//   - scroll: a scrolled sidebar keeps its scrollTop across a sibling
//     navigation inside the shell (the DOM node never unmounts; the
//     stamp below proves it is the same node, not a re-rendered twin).
//   - open group: navigating deep into a collapsed group's section
//     opens the group's <details> for its current child (the
//     activelink sweep's group arm, no request), and it stays open
//     across a sibling navigation.
//
// The audit's mutations: making the nav a RouteArea re-renders it per
// navigation and resets scrollTop (and the group) — both arms fail.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestSidebarKeepsScrollAndOpenGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	url := trackerServe(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))

	// Make the sidebar scrollable (the audit's rig note: overflow:auto
	// content tall enough to scroll), stamp the DOM node so a
	// re-render cannot masquerade as persistence, and COLLAPSE the
	// Projects group so the navigation must open it.
	setup := `(() => {
		const nav = document.querySelector('.fui-content-row__nav nav');
		nav.setAttribute('data-keep-stamp', 'same-node');
		const side = document.querySelector('.fui-content-row__nav');
		side.style.overflow = 'auto';
		side.style.height = '120px';
		side.scrollTop = 60;
		const group = nav.querySelector('details');
		group.open = false;
		return true;
	})()`
	var stamp, groupOpenAfterDeep, groupOpenAfterSibling string
	var scrollAfterDeep, scrollAfterSibling int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		chromedp.Evaluate(setup, nil),

		// Deep navigation into the (now collapsed) group's section,
		// through the home page's own project card — the sidebar's
		// link is hidden inside the closed group, which is the point.
		chromedp.Click(`main a[href="/projects/billing"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#filter-search-filter`, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav nav')?.getAttribute('data-keep-stamp') || ''`, &stamp),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav nav details')?.open ? 'open' : 'closed'`, &groupOpenAfterDeep),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav')?.scrollTop ?? -1`, &scrollAfterDeep),

		// Sibling navigation within the same group: the layer (and the
		// sidebar with it) is kept. The click is dispatched in JS so
		// chromedp's scroll-into-view does not move the very scrollTop
		// under test (the delegated handler catches it all the same).
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav a[href="/projects/search"]').click(); true`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav nav')?.getAttribute('data-keep-stamp') || ''`, &stamp),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav nav details')?.open ? 'open' : 'closed'`, &groupOpenAfterSibling),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav')?.scrollTop ?? -1`, &scrollAfterSibling),
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if stamp != "same-node" {
		t.Errorf("sidebar nav stamp = %q, want same-node — the kept layer re-rendered the sidebar", stamp)
	}
	if scrollAfterDeep != 60 || scrollAfterSibling != 60 {
		t.Errorf("sidebar scrollTop: after deep nav = %d, after sibling nav = %d, want 60/60 (kept)", scrollAfterDeep, scrollAfterSibling)
	}
	if groupOpenAfterDeep != "open" {
		t.Errorf("group after deep navigation = %s, want open (activelink opens the current link's group)", groupOpenAfterDeep)
	}
	if groupOpenAfterSibling != "open" {
		t.Errorf("group after sibling navigation = %s, want open (stays open across a sibling nav)", groupOpenAfterSibling)
	}
}
