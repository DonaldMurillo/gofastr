package main

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The pointer-modality contract of the swap focus (frag/nav.js
// _navPointer): a navigation that began from a real mouse click
// focuses the fresh cell with focusVisible:false — the ring is
// keyboard signal, not noise — while a keyboard activation keeps it.
//
// Both tests drive the browser through CDP input (Input.dispatchMouseEvent
// / KeyEvent), never el.click(): a synthetic el.click() carries
// detail 0, the exact shape a keyboard activation has, so it cannot
// even express the distinction under test.

// mouseClickAt presses and releases the left button at viewport
// coordinates (x, y): the browser synthesizes the same event shape a
// physical mouse produces (detail 1, real hit testing).
func mouseClickAt(t *testing.T, ctx context.Context, x, y float64) {
	t.Helper()
	click := func(p *input.DispatchMouseEventParams) {
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			return p.Do(c)
		})); err != nil {
			t.Fatalf("dispatch mouse event: %v", err)
		}
	}
	click(input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1))
	click(input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(input.Left).WithClickCount(1))
}

// nodeCenter returns the viewport centre of the first element
// matching sel.
func nodeCenter(t *testing.T, ctx context.Context, sel string) (float64, float64) {
	t.Helper()
	var box struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"width"`
		H float64 `json:"height"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const r = document.querySelector(`+"`"+sel+"`"+`).getBoundingClientRect();`+
			`return {x:r.x, y:r.y, width:r.width, height:r.height}; })()`, &box)); err != nil {
		t.Fatalf("box %s: %v", sel, err)
	}
	if box.W == 0 || box.H == 0 {
		t.Fatalf("%s has no box (%v×%v) — element not visible", sel, box.W, box.H)
	}
	return box.X + box.W/2, box.Y + box.H/2
}

// focusVisible reports whether the active element paints the focus
// ring.
func focusVisible(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var ring bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!(document.activeElement && document.activeElement.matches(':focus-visible'))`, &ring)); err != nil {
		t.Fatalf("read :focus-visible: %v", err)
	}
	return ring
}

// TestTrackerSupersedingPointerClicksKeepRingOff: two real mouse
// clicks on two issue rows ~40ms apart. The first (cached) navigation
// commits at once and its view transition is still running when the
// second click lands — the click hit-tests to <html>, the transition
// module re-delivers it to the row under the pointer, and the
// re-delivered navigation must keep the POINTER modality: no focus
// ring on the issue panel it lands. A single mouse click already
// behaves (detail 1 reaches the handler); the re-delivery is the only
// path that loses it.
func TestTrackerSupersedingPointerClicksKeepRingOff(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	// Billing's list, then cache BIL-31's page and come Back, so the
	// first of the two clicks replays from the screen cache (an
	// in-flight fetch would not have committed — no transition to
	// interrupt — and the second click would be an ordinary one).
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail`, chromedp.ByQuery),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Poll(`window.__gofastr.currentPath === '/projects/billing'`, nil),
		chromedp.WaitVisible(`a.fui-card[data-key][data-key="BIL-63"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	x1, y1 := nodeCenter(t, ctx, `a.fui-card[data-key][data-key="BIL-31"]`)
	x2, y2 := nodeCenter(t, ctx, `a.fui-card[data-key][data-key="BIL-63"]`)
	if err := chromedp.Run(ctx, page.BringToFront()); err != nil {
		t.Fatalf("bring to front: %v", err)
	}
	mouseClickAt(t, ctx, x1, y1)
	if err := chromedp.Run(ctx, chromedp.Sleep(40*time.Millisecond)); err != nil {
		t.Fatalf("gap: %v", err)
	}
	mouseClickAt(t, ctx, x2, y2)

	// The second navigation lands on BIL-63; let its transition and
	// focus settle before reading the ring.
	waitText(t, ctx, "#issue-detail h2", "Dunning emails stop after the first reminder")
	if err := chromedp.Run(ctx,
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Poll(`window.__gofastr.currentPath === '/projects/billing/issues/63'`, nil),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if ring := focusVisible(t, ctx); ring {
		t.Error("the focused element matches :focus-visible after two pointer clicks; " +
			"a pointer-initiated navigation must not paint the focus ring")
	}
}

// TestTrackerKeyboardNavigationKeepsFocusRing: Enter on a focused
// issue link is a keyboard activation — the swap's focus on the fresh
// cell must carry the ring.
func TestTrackerKeyboardNavigationKeepsFocusRing(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('a.fui-card[data-key][data-key="BIL-31"]').focus()`, nil),
		page.BringToFront(),
		chromedp.KeyEvent(kb.Enter),
	); err != nil {
		t.Fatalf("keyboard navigate: %v", err)
	}
	waitText(t, ctx, "#issue-detail h2", "Proration credit drops the last day of a mid-month plan change")
	if err := chromedp.Run(ctx,
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Poll(`window.__gofastr.currentPath === '/projects/billing/issues/31'`, nil),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if ring := focusVisible(t, ctx); !ring {
		t.Error("no focused element matches :focus-visible after a keyboard navigation; " +
			"the focus ring is the keyboard's signal and must stay on")
	}
}
