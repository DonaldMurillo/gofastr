package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Overlay Behavioral Test Suite
// =============================================================================
//
// These tests verify HOW overlays should behave from a user's perspective.
// They do NOT test implementation details like variable names or CSS class names.
//
// Behavioral contracts tested:
//   1. Each overlay type opens with correct content
//   2. Every close mechanism works (Escape, ×, Cancel/Confirm, backdrop click)
//   3. Clicking inside content does NOT close (except close buttons)
//   4. Body scroll is locked while open, restored when all close
//   5. Multiple overlays stack, close in LIFO order
//   6. Focus moves into overlay, Tab is trapped inside
//   7. No stale DOM after navigation or hard refresh
//   8. Edge cases: rapid clicks, reopen, Escape with none open
// =============================================================================

// =============================================================================
// 1. OPEN BEHAVIOR — each type renders correct content
// =============================================================================

func TestOverlay_DrawerOpensWithNavContent(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-overlay]:not([hidden])')?.textContent ?? ''`, &text),
	)
	if err != nil {
		t.Fatalf("drawer open: %v", err)
	}
	for _, want := range []string{"Quick Nav", "Home", "Products", "About"} {
		if !strings.Contains(text, want) {
			t.Errorf("drawer should contain %q", want)
		}
	}
}

func TestOverlay_SheetOpensWithProductPreview(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-sheet"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-overlay]:not([hidden])')?.textContent ?? ''`, &text),
	)
	if err != nil {
		t.Fatalf("sheet open: %v", err)
	}
	for _, want := range []string{"Quick Preview", "Widget Pro", "$29.99"} {
		if !strings.Contains(text, want) {
			t.Errorf("sheet should contain %q", want)
		}
	}
}

func TestOverlay_DialogOpensWithConfirmPrompt(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-overlay]:not([hidden])')?.textContent ?? ''`, &text),
	)
	if err != nil {
		t.Fatalf("dialog open: %v", err)
	}
	for _, want := range []string{"Confirm Action", "Cancel", "Confirm"} {
		if !strings.Contains(text, want) {
			t.Errorf("dialog should contain %q", want)
		}
	}
}

func TestOverlay_OnlyOneOverlayAfterSingleOpen(t *testing.T) {
	for _, overlayType := range []string{"drawer", "sheet", "dialog"} {
		t.Run(overlayType, func(t *testing.T) {
			base := startTestServer(t)
			ctx := newBrowserCtx(t)

			var count int
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				waitForPage(),
				chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &count),
			)
			if err != nil {
				t.Fatalf("%s: %v", overlayType, err)
			}
			if count != 1 {
				t.Errorf("%s: expected exactly 1 overlay, got %d", overlayType, count)
			}
		})
	}
}

// =============================================================================
// 2. CLOSE MECHANISMS — every way to close each overlay type
// =============================================================================

func TestOverlay_EscapeClosesDrawer(t *testing.T) {
	testEscapeCloses(t, "drawer")
}
func TestOverlay_EscapeClosesSheet(t *testing.T) {
	testEscapeCloses(t, "sheet")
}
func TestOverlay_EscapeClosesDialog(t *testing.T) {
	testEscapeCloses(t, "dialog")
}

func testEscapeCloses(t *testing.T, overlayType string) {
	t.Helper()
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var afterClose int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
	)
	if err != nil {
		t.Fatalf("%s escape: %v", overlayType, err)
	}
	if afterClose != 0 {
		t.Errorf("%s: Escape should close overlay, %d remain", overlayType, afterClose)
	}
}

func TestOverlay_XButtonClosesEachType(t *testing.T) {
	for _, overlayType := range []string{"drawer", "sheet", "dialog"} {
		t.Run(overlayType, func(t *testing.T) {
			base := startTestServer(t)
			ctx := newBrowserCtx(t)

			var afterClose int
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				waitForPage(),
				chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				// The × button has class "overlay-close" and attr "data-overlay-close"
				chromedp.Evaluate(`document.querySelector('[data-overlay]:not([hidden]) .overlay-close')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
			)
			if err != nil {
				t.Fatalf("%s ×: %v", overlayType, err)
			}
			if afterClose != 0 {
				t.Errorf("%s: × button should close overlay, %d remain", overlayType, afterClose)
			}
		})
	}
}

func TestOverlay_BackdropClickClosesEachType(t *testing.T) {
	for _, overlayType := range []string{"drawer", "sheet", "dialog"} {
		t.Run(overlayType, func(t *testing.T) {
			base := startTestServer(t)
			ctx := newBrowserCtx(t)

			var afterClose int
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				waitForPage(),
				chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				// Click the backdrop element directly (not its content child)
				chromedp.Evaluate(`document.querySelector('.overlay-backdrop')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
			)
			if err != nil {
				t.Fatalf("%s backdrop: %v", overlayType, err)
			}
			if afterClose != 0 {
				t.Errorf("%s: clicking backdrop should close overlay, %d remain", overlayType, afterClose)
			}
		})
	}
}

func TestOverlay_CancelButtonClosesDialog(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var afterClose int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-overlay-close]')).find(b => b.textContent.includes('Cancel'))?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
	)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if afterClose != 0 {
		t.Errorf("Cancel button should close dialog, %d remain", afterClose)
	}
}

func TestOverlay_ConfirmButtonClosesDialog(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var afterClose int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-overlay-close]')).find(b => b.textContent.includes('Confirm'))?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
	)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if afterClose != 0 {
		t.Errorf("Confirm button should close dialog, %d remain", afterClose)
	}
}

func TestOverlay_SheetCloseButtonWorks(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var afterClose int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-sheet"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.sheet-close-btn')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
	)
	if err != nil {
		t.Fatalf("sheet close btn: %v", err)
	}
	if afterClose != 0 {
		t.Errorf("sheet Close button should close it, %d remain", afterClose)
	}
}

func TestOverlay_DrawerCloseButtonWorks(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var afterClose int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.drawer-close-btn')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &afterClose),
	)
	if err != nil {
		t.Fatalf("drawer close btn: %v", err)
	}
	if afterClose != 0 {
		t.Errorf("drawer Close button should close it, %d remain", afterClose)
	}
}

// =============================================================================
// 3. CLICKING CONTENT DOES NOT CLOSE
// =============================================================================

func TestOverlay_ClickInsideDrawerDoesNotClose(t *testing.T) {
	testClickInsideNoClose(t, "drawer")
}
func TestOverlay_ClickInsideSheetDoesNotClose(t *testing.T) {
	testClickInsideNoClose(t, "sheet")
}
func TestOverlay_ClickInsideDialogDoesNotClose(t *testing.T) {
	testClickInsideNoClose(t, "dialog")
}

func testClickInsideNoClose(t *testing.T, overlayType string) {
	t.Helper()
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var countAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Click the heading inside the overlay content
		chromedp.Evaluate(`document.querySelector('[data-overlay] h2')?.click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &countAfter),
	)
	if err != nil {
		t.Fatalf("%s content click: %v", overlayType, err)
	}
	if countAfter != 1 {
		t.Errorf("%s: clicking inside content should NOT close, got %d overlays", overlayType, countAfter)
	}
}

// =============================================================================
// 4. SCROLL LOCK — body overflow management
// =============================================================================

func TestOverlay_BodyScrollLockedWhileOpen(t *testing.T) {
	for _, overlayType := range []string{"drawer", "sheet", "dialog"} {
		t.Run(overlayType, func(t *testing.T) {
			base := startTestServer(t)
			ctx := newBrowserCtx(t)

			var overflow string
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				waitForPage(),
				chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.body.style.overflow`, &overflow),
			)
			if err != nil {
				t.Fatalf("%s scroll lock: %v", overlayType, err)
			}
			if overflow != "hidden" {
				t.Errorf("%s: body overflow should be 'hidden', got %q", overlayType, overflow)
			}
		})
	}
}

func TestOverlay_ScrollRestoredAfterClose(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var overflow string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.body.style.overflow`, &overflow),
	)
	if err != nil {
		t.Fatalf("scroll restore: %v", err)
	}
	if overflow == "hidden" {
		t.Errorf("body overflow should be restored after close, still 'hidden'")
	}
}

func TestOverlay_ScrollLockPersistsWhileStacked(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var afterFirstClose string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Open drawer, then stack dialog on top via JS (backdrop blocks clicks)
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('confirm-dialog')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Close topmost
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.body.style.overflow`, &afterFirstClose),
	)
	if err != nil {
		t.Fatalf("scroll stack: %v", err)
	}
	if afterFirstClose != "hidden" {
		t.Errorf("scroll lock should persist while drawer still open, got %q", afterFirstClose)
	}
}

func TestOverlay_ScrollRestoredAfterAllStackedClosed(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var overflowFinal string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('confirm-dialog')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Close both
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.body.style.overflow`, &overflowFinal),
	)
	if err != nil {
		t.Fatalf("scroll restore stack: %v", err)
	}
	if overflowFinal == "hidden" {
		t.Errorf("scroll should be restored after all overlays closed, still %q", overflowFinal)
	}
}

// =============================================================================
// 5. STACKING — multiple overlays, LIFO close order
// =============================================================================

func TestOverlay_StackTwoOverlays(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('confirm-dialog')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &count),
	)
	if err != nil {
		t.Fatalf("stack 2: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 stacked overlays, got %d", count)
	}
}

func TestOverlay_StackThreeOverlays(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('demo-sheet')`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('confirm-dialog')`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &count),
	)
	if err != nil {
		t.Fatalf("stack 3: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 stacked overlays, got %d", count)
	}
}

func TestOverlay_LIFOClose(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var after1, after2, after3 int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Open 3 overlays
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('demo-sheet')`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.openWidget('confirm-dialog')`, nil),
		chromedp.Sleep(400*time.Millisecond),
		// Escape 1 → closes dialog
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &after1),
		// Escape 2 → closes sheet
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &after2),
		// Escape 3 → closes drawer
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &after3),
	)
	if err != nil {
		t.Fatalf("LIFO: %v", err)
	}
	if after1 != 2 {
		t.Errorf("after 1st Escape: expected 2, got %d", after1)
	}
	if after2 != 1 {
		t.Errorf("after 2nd Escape: expected 1, got %d", after2)
	}
	if after3 != 0 {
		t.Errorf("after 3rd Escape: expected 0, got %d", after3)
	}
}

// =============================================================================
// 6. FOCUS MANAGEMENT
// =============================================================================

func TestOverlay_FocusMovesIntoOverlay(t *testing.T) {
	for _, overlayType := range []string{"drawer", "sheet", "dialog"} {
		t.Run(overlayType, func(t *testing.T) {
			base := startTestServer(t)
			ctx := newBrowserCtx(t)

			var focusedInside bool
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				waitForPage(),
				chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.activeElement?.closest('[data-overlay]') !== null`, &focusedInside),
			)
			if err != nil {
				t.Fatalf("%s focus: %v", overlayType, err)
			}
			if !focusedInside {
				var tag string
				chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement?.tagName ?? 'NONE'`, &tag))
				t.Errorf("%s: focus should move into overlay, active element is <%s>", overlayType, tag)
			}
		})
	}
}

func TestOverlay_TabTrapCyclesWithinOverlay(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var stillInside bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Focus last focusable element, then Tab (should wrap to first)
		chromedp.Evaluate(`const f=document.querySelectorAll('[data-overlay] button,[data-overlay] a,[data-overlay] input'); if(f.length>0) f[f.length-1].focus()`, nil),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true}))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.activeElement?.closest('[data-overlay]') !== null`, &stillInside),
	)
	if err != nil {
		t.Fatalf("tab trap: %v", err)
	}
	if !stillInside {
		t.Error("Tab from last focusable should wrap back into overlay, not escape to page")
	}
}

func TestOverlay_ShiftTabTrapCyclesWithinOverlay(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var stillInside bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Focus first focusable element, then Shift+Tab (should wrap to last)
		chromedp.Evaluate(`const f=document.querySelectorAll('[data-overlay] button,[data-overlay] a,[data-overlay] input'); if(f.length>0) f[0].focus()`, nil),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', shiftKey: true, bubbles: true}))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.activeElement?.closest('[data-overlay]') !== null`, &stillInside),
	)
	if err != nil {
		t.Fatalf("shift+tab trap: %v", err)
	}
	if !stillInside {
		t.Error("Shift+Tab from first focusable should wrap to last inside overlay")
	}
}

// =============================================================================
// 7. DOM HYGIENE
// =============================================================================

func TestOverlay_DOMRemovedAfterCloseAnimation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(600*time.Millisecond), // wait for 300ms animation
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &count),
	)
	if err != nil {
		t.Fatalf("DOM cleanup: %v", err)
	}
	if count != 0 {
		t.Errorf("overlay DOM should be removed after close, %d remain", count)
	}
}

func TestOverlay_NoStaleDOMAfterClientNav(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var countAfterNav int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Client-side navigate away
		chromedp.Evaluate(`document.querySelector('a[href="/about"]')?.click()`, nil),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &countAfterNav),
	)
	if err != nil {
		t.Fatalf("nav cleanup: %v", err)
	}
	if countAfterNav > 0 {
		t.Errorf("overlays should be gone after navigation, %d remain", countAfterNav)
	}
}

func TestOverlay_NoStaleDOMAfterHardRefresh(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var countAfterRefresh int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Hard refresh
		chromedp.Navigate(base+"/"),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &countAfterRefresh),
	)
	if err != nil {
		t.Fatalf("hard refresh: %v", err)
	}
	if countAfterRefresh > 0 {
		t.Errorf("no overlays should survive hard refresh, got %d", countAfterRefresh)
	}
}

// =============================================================================
// 8. EDGE CASES
// =============================================================================

func TestOverlay_EscapeWithNoneOpenIsNoop(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`for(let i=0;i<10;i++){document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))}`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('main')?.textContent ?? ''`, &mainText),
	)
	if err != nil {
		t.Fatalf("escape noop: %v", err)
	}
	if !strings.Contains(mainText, "Welcome to GoFastr") {
		t.Error("page should be intact after mashing Escape with no overlays")
	}
}

func TestOverlay_RapidClicksDoNotLeakOverlays(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Click Open Dialog 5 times rapidly
		chromedp.Evaluate(`for(let i=0;i<5;i++){document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()}`, nil),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &count),
	)
	if err != nil {
		t.Fatalf("rapid clicks: %v", err)
	}
	if count > 3 {
		t.Errorf("rapid clicks created %d overlays — should dedupe or debounce", count)
	}
	// Cleanup
	chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').forEach(el=>el.remove())`, nil),
		chromedp.Evaluate(`document.body.style.overflow=''`, nil),
	)
}

func TestOverlay_CloseOverlayNoopWhenNoneOpen(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`window.__gofastr.closeWidget('confirm-dialog')`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('main')?.textContent ?? ''`, &mainText),
	)
	if err != nil {
		t.Fatalf("closeWidget noop: %v", err)
	}
	if !strings.Contains(mainText, "Welcome to GoFastr") {
		t.Error("closeWidget with none open should be a noop")
	}
}

func TestOverlay_ReopenAfterClose(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var countAfterReopen int
	var textAfterReopen string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Reopen
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &countAfterReopen),
		chromedp.Evaluate(`document.querySelector('[data-overlay]:not([hidden])')?.textContent ?? ''`, &textAfterReopen),
	)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if countAfterReopen != 1 {
		t.Errorf("reopened overlay should be 1, got %d", countAfterReopen)
	}
	if !strings.Contains(textAfterReopen, "Quick Nav") {
		t.Error("reopened drawer should contain correct content")
	}
}

func TestOverlay_DrawerLinkNavigatesAndClosesOverlay(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var url string
	var countAfterNav int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Click About link inside drawer
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay] a').forEach(a => { if(a.textContent.includes('About')) a.click() })`, nil),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Location(&url),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &countAfterNav),
	)
	if err != nil {
		t.Fatalf("drawer nav: %v", err)
	}
	if !strings.Contains(url, "/about") {
		t.Errorf("drawer link should navigate to /about, got %s", url)
	}
	if countAfterNav > 0 {
		t.Errorf("overlays should be gone after drawer link navigation, %d remain", countAfterNav)
	}
}

// =============================================================================
// 9. STRUCTURAL — correct elements exist per type
// =============================================================================

func TestOverlay_SheetHasDragHandle(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasHandle bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-sheet"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.sheet-handle') !== null`, &hasHandle),
	)
	if err != nil {
		t.Fatalf("sheet handle: %v", err)
	}
	if !hasHandle {
		t.Error("sheet should have a drag handle element")
	}
}

func TestOverlay_DrawerHasMultipleNavLinks(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var linkCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-drawer"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay] a').length`, &linkCount),
	)
	if err != nil {
		t.Fatalf("drawer links: %v", err)
	}
	if linkCount < 4 {
		t.Errorf("drawer should have at least 4 nav links, got %d", linkCount)
	}
}

func TestOverlay_DialogHasCancelAndConfirmButtons(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasCancel, hasConfirm bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="confirm-dialog"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-overlay] button')).some(b => b.textContent.includes('Cancel'))`, &hasCancel),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-overlay] button')).some(b => b.textContent.includes('Confirm'))`, &hasConfirm),
	)
	if err != nil {
		t.Fatalf("dialog buttons: %v", err)
	}
	if !hasCancel {
		t.Error("dialog should have a Cancel button")
	}
	if !hasConfirm {
		t.Error("dialog should have a Confirm button")
	}
}

func TestOverlay_EachTypeHasXCloseButton(t *testing.T) {
	for _, overlayType := range []string{"drawer", "sheet", "dialog"} {
		t.Run(overlayType, func(t *testing.T) {
			base := startTestServer(t)
			ctx := newBrowserCtx(t)

			var hasX bool
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				waitForPage(),
				chromedp.Evaluate(`document.querySelector('button[data-fui-open*="`+overlayType+`"]')?.click()`, nil),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.querySelector('[data-overlay] .overlay-close') !== null`, &hasX),
			)
			if err != nil {
				t.Fatalf("%s ×: %v", overlayType, err)
			}
			if !hasX {
				t.Errorf("%s should have a × close button", overlayType)
			}
		})
	}
}

// =============================================================================
// 10. CACHE — reopening uses cached HTML
// =============================================================================

func TestOverlay_ReopenFromCacheWorks(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var countAfterReopen int
	var textAfterReopen string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-sheet"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(600*time.Millisecond),
		// Reopen — should use cached HTML
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="demo-sheet"]')?.click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]:not([hidden])').length`, &countAfterReopen),
		chromedp.Evaluate(`document.querySelector('[data-overlay]:not([hidden])')?.textContent ?? ''`, &textAfterReopen),
	)
	if err != nil {
		t.Fatalf("cache reopen: %v", err)
	}
	if countAfterReopen != 1 {
		t.Errorf("reopened sheet from cache should be 1 overlay, got %d", countAfterReopen)
	}
	if !strings.Contains(textAfterReopen, "Quick Preview") {
		t.Error("reopened sheet from cache should contain correct content")
	}
}
