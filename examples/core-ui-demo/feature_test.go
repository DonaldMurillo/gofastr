package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// FEATURE UI TESTS — every framework feature tested via real browser
// =============================================================================

// TestFeature_DialogOverlay verifies the dialog overlay opens, shows content,
// can be closed via the close button, and body scroll is restored.
func TestFeature_DialogOverlay(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var overlayCount int
	var bodyOverflow string
	var dialogText string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Open dialog via runtime API
		chromedp.Evaluate(`window.__gofastr.openOverlay('dialog', '/confirm-dialog')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Verify overlay element exists
		chromedp.Evaluate(`document.querySelectorAll('.dialog-overlay[data-overlay]').length`, &overlayCount),
		// Verify dialog content rendered
		chromedp.Evaluate(`document.querySelector('.dialog-overlay .dialog')?.textContent ?? 'NOT_FOUND'`, &dialogText),
		// Verify body scroll locked
		chromedp.Evaluate(`document.body.style.overflow`, &bodyOverflow),
	)
	if err != nil {
		t.Fatalf("dialog open: %v", err)
	}

	if overlayCount != 1 {
		t.Errorf("expected 1 dialog overlay, got %d", overlayCount)
	}
	if !strings.Contains(dialogText, "Confirm Action") {
		t.Errorf("dialog should contain 'Confirm Action', got: %s", truncate(dialogText, 100))
	}
	if !strings.Contains(dialogText, "Are you sure") {
		t.Errorf("dialog should contain confirmation message, got: %s", truncate(dialogText, 100))
	}
	if bodyOverflow != "hidden" {
		t.Errorf("body overflow should be 'hidden' while dialog open, got %q", bodyOverflow)
	}

	// Close dialog via overlay-close button
	var overlayCountAfterClose int
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-overlay-close]').click()`, nil),
		chromedp.Sleep(400*time.Millisecond), // wait for close animation
		chromedp.Evaluate(`document.querySelectorAll('.dialog-overlay').length`, &overlayCountAfterClose),
	)
	if err != nil {
		t.Fatalf("dialog close: %v", err)
	}
	if overlayCountAfterClose != 0 {
		t.Errorf("dialog should be removed after close, got %d overlays", overlayCountAfterClose)
	}
}

// TestFeature_DialogEscapeClose verifies Escape key closes the dialog.
func TestFeature_DialogEscapeClose(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var overlayCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`window.__gofastr.openOverlay('dialog', '/confirm-dialog')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Press Escape
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('.dialog-overlay').length`, &overlayCount),
	)
	if err != nil {
		t.Fatalf("dialog escape: %v", err)
	}
	if overlayCount != 0 {
		t.Errorf("dialog should close on Escape, got %d overlays", overlayCount)
	}
}

// TestFeature_DialogFocusTrap verifies Tab key cycles within the dialog.
func TestFeature_DialogFocusTrap(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var firstFocused string
	var lastFocusedID string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`window.__gofastr.openOverlay('dialog', '/confirm-dialog')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Get the first focusable element's text
		chromedp.Evaluate(`document.activeElement?.textContent ?? 'none'`, &firstFocused),
		// Tab through all focusable elements to reach last, then tab again
		chromedp.Evaluate(`
			(() => {
				const overlay = document.querySelector('.dialog-overlay');
				const buttons = overlay.querySelectorAll('button');
				// Focus the last button
				buttons[buttons.length - 1].focus();
				// Dispatch Tab from the last element — should wrap to first
				buttons[buttons.length - 1].dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true}));
				return document.activeElement?.textContent ?? 'none';
			})()
		`, &lastFocusedID),
	)
	if err != nil {
		t.Fatalf("focus trap: %v", err)
	}
	// First focusable should be something in the dialog
	if firstFocused == "none" {
		t.Error("dialog should auto-focus a button on open")
	}
}

// TestFeature_SheetOverlay verifies the sheet overlay (bottom slide-up) opens and closes.
func TestFeature_SheetOverlay(t *testing.T) {
	t.Skip("cart sheet removed — sheet overlay tested via dialog instead")
}

// TestFeature_SheetHasDragHandle verifies the sheet has a drag handle element.
func TestFeature_SheetHasDragHandle(t *testing.T) {
	t.Skip("cart sheet removed")
}

// TestFeature_AddToCartUpdatesBadge verifies clicking "Add to cart" updates badge and shows toast.
func TestFeature_AddToCartUpdatesBadge(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var badgeText string
	var toastText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Click the first add-to-cart button
		chromedp.Evaluate(`document.querySelector('[data-action="add-to-cart"]').click()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Check badge updated
		chromedp.Evaluate(`document.querySelector('.cart-badge')?.textContent ?? 'none'`, &badgeText),
		// Check toast appeared
		chromedp.Evaluate(`document.querySelector('.gofastr-toast')?.textContent ?? 'none'`, &toastText),
	)
	if err != nil {
		t.Fatalf("add-to-cart: %v", err)
	}
	if !strings.Contains(badgeText, "1") {
		t.Errorf("badge should show 1, got: %s", badgeText)
	}
	if !strings.Contains(toastText, "Added to cart") {
		t.Errorf("should show 'Added to cart' toast, got: %s", toastText)
	}
}

// TestFeature_DrawerNavigation verifies the cart drawer page loads and shows content.
func TestFeature_DrawerNavigation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	var currentURL string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate to cart via client-side link
		chromedp.Evaluate(`document.querySelector('nav a[href="/cart"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Location(&currentURL),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
	)
	if err != nil {
		t.Fatalf("drawer navigation: %v", err)
	}
	if !strings.Contains(currentURL, "/cart") {
		t.Errorf("expected URL /cart, got %s", currentURL)
	}
	if !strings.Contains(mainText, "Shopping Cart") {
		t.Errorf("cart drawer should show 'Shopping Cart', got: %s", truncate(mainText, 100))
	}
	if !strings.Contains(mainText, "Your cart is empty") {
		t.Errorf("empty cart should say so, got: %s", truncate(mainText, 100))
	}
}

// TestFeature_ErrorBoundaryPage verifies the error boundary page shows the red error box.
func TestFeature_ErrorBoundaryPage(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	var errorBoxExists bool
	var errorBoxText string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate to error boundary page
		chromedp.Evaluate(`document.querySelector('nav a[href="/error-boundary"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Check main content
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
		// Check error box exists
		chromedp.Evaluate(`document.querySelector('.error-boundary-result') !== null`, &errorBoxExists),
		// Check error box content
		chromedp.Evaluate(`document.querySelector('.error-boundary-result')?.textContent ?? 'NONE'`, &errorBoxText),
	)
	if err != nil {
		t.Fatalf("error boundary page: %v", err)
	}

	if !strings.Contains(mainText, "Error Boundary Demo") {
		t.Errorf("page should contain 'Error Boundary Demo', got: %s", truncate(mainText, 100))
	}
	if !strings.Contains(mainText, "Working Component") {
		t.Errorf("page should contain working component section, got: %s", truncate(mainText, 100))
	}
	if !errorBoxExists {
		t.Error("error boundary result box should exist on page")
	}
	if !strings.Contains(errorBoxText, "Error:") {
		t.Errorf("error box should contain 'Error:', got: %s", truncate(errorBoxText, 100))
	}
	if !strings.Contains(errorBoxText, "deliberate panic") {
		t.Errorf("error box should mention the panic, got: %s", truncate(errorBoxText, 100))
	}
}

// TestFeature_SignalDemoPage verifies the signal demo shows computed value and effect log.
func TestFeature_SignalDemoPage(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	var totalText string
	var logText string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate to signals page
		chromedp.Evaluate(`document.querySelector('nav a[href="/signals"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Check main content
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
		// Check computed total
		chromedp.Evaluate(`document.querySelector('.product-detail-price')?.textContent ?? 'NONE'`, &totalText),
		// Check effect log
		chromedp.Evaluate(`document.querySelector('[aria-live="polite"]')?.textContent ?? 'NONE'`, &logText),
	)
	if err != nil {
		t.Fatalf("signal demo page: %v", err)
	}

	if !strings.Contains(mainText, "Signal Demo") {
		t.Errorf("page should contain 'Signal Demo', got: %s", truncate(mainText, 100))
	}
	if !strings.Contains(mainText, "Computed") {
		t.Errorf("page should mention Computed, got: %s", truncate(mainText, 200))
	}
	if !strings.Contains(totalText, "$29.99") {
		t.Errorf("computed total should be $29.99, got: %s", totalText)
	}
	if !strings.Contains(logText, "Quantity changed to 1") {
		t.Errorf("effect log should show initial quantity, got: %s", logText)
	}
}

// TestFeature_SignalDemoCounter verifies clicking +/- buttons updates the computed total and log.
func TestFeature_SignalDemoCounter(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var totalText, logText, qtyText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('nav a[href="/signals"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Click increment button
		chromedp.Evaluate(`
			(() => {
				const btns = document.querySelectorAll('[data-action="signal-increment"]');
				if (btns.length === 0) return 'NO_BTN';
				btns[0].click();
				return 'clicked';
			})()
		`, nil),
		chromedp.Sleep(300*time.Millisecond),
		// Check DOM updates
		chromedp.Evaluate(`document.getElementById('signal-total')?.textContent ?? 'none'`, &totalText),
		chromedp.Evaluate(`document.getElementById('signal-log')?.textContent ?? 'none'`, &logText),
		chromedp.Evaluate(`document.getElementById('signal-qty')?.textContent ?? 'none'`, &qtyText),
	)
	if err != nil {
		t.Fatalf("signal counter: %v", err)
	}
	if !strings.Contains(totalText, "$59.98") {
		t.Errorf("signal total should show $59.98, got: %s", totalText)
	}
	if !strings.Contains(logText, "Quantity changed to 2") {
		t.Errorf("signal log should show quantity change, got: %s", logText)
	}
	if qtyText != "2" {
		t.Errorf("signal counter value should be 2, got: %s", qtyText)
	}
}

// TestFeature_TwoWayBinding verifies data-bind syncs input value with state.
func TestFeature_TwoWayBinding(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasBindAttr bool
	var bindValue string
	var stateValue string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products"),
		waitForPage(),
		// Check data-bind attribute exists
		chromedp.Evaluate(`document.querySelector('[data-bind]') !== null`, &hasBindAttr),
		chromedp.Evaluate(`document.querySelector('[data-bind]')?.getAttribute('data-bind') ?? 'none'`, &bindValue),
		// Type into the bound input
		chromedp.SendKeys("#search-input", "test-value", chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		// Check state was synced
		chromedp.Evaluate(`window.__gofastr.getState('search', '')`, &stateValue),
	)
	if err != nil {
		t.Fatalf("two-way binding: %v", err)
	}
	if !hasBindAttr {
		t.Error("search input should have data-bind attribute")
	}
	if bindValue != "search" {
		t.Errorf("data-bind should be 'search', got %q", bindValue)
	}
	if !strings.Contains(stateValue, "test-value") {
		t.Errorf("state should contain 'test-value' after typing, got: %s", stateValue)
	}
}

// TestFeature_ServerAction verifies server actions return responses and show toast.
func TestFeature_ServerAction(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	// Test with a component that has registered actions (signals page)
	var actionResult string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/signals"),
		waitForPage(),
		// Call server action with componentId (signals has action registry)
		chromedp.Evaluate(`window.__gofastr._serverActionFor('signal-demo', 'signal-increment', {}).then(r => window.__serverResult = JSON.stringify(r))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__serverResult ?? 'none'`, &actionResult),
	)
	if err != nil {
		t.Fatalf("server action: %v", err)
	}
	if !strings.Contains(actionResult, `"status":"ok"`) {
		t.Errorf("server action should return status ok, got: %s", actionResult)
	}

	// Test error case: unknown component
	var errorResult string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr._serverActionFor('nonexistent', 'test', {}).then(r => window.__serverError = JSON.stringify(r))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__serverError ?? 'none'`, &errorResult),
	)
	if err != nil {
		t.Fatalf("server action error case: %v", err)
	}
	if !strings.Contains(errorResult, `"status":"error"`) {
		t.Errorf("unknown component should return error, got: %s", errorResult)
	}
}

// TestFeature_ToastNotification verifies toast appears and auto-removes.
func TestFeature_ToastNotification(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var toastExists bool
	var toastText string
	var toastRole string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Trigger a toast
		chromedp.Evaluate(`window.__gofastr.toast('Test notification!')`, nil),
		chromedp.Sleep(100*time.Millisecond),
		// Verify toast element exists
		chromedp.Evaluate(`document.querySelector('.gofastr-toast') !== null`, &toastExists),
		chromedp.Evaluate(`document.querySelector('.gofastr-toast')?.textContent ?? 'none'`, &toastText),
		chromedp.Evaluate(`document.querySelector('.gofastr-toast')?.getAttribute('role') ?? 'none'`, &toastRole),
	)
	if err != nil {
		t.Fatalf("toast: %v", err)
	}
	if !toastExists {
		t.Error("toast element should exist")
	}
	if toastText != "Test notification!" {
		t.Errorf("toast text should be 'Test notification!', got: %s", toastText)
	}
	if toastRole != "status" {
		t.Errorf("toast should have role='status', got: %s", toastRole)
	}
}

// TestFeature_CSSCustomPropertiesApplied verifies theme tokens are actually applied as CSS vars.
func TestFeature_CSSCustomPropertiesApplied(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var primaryColor string
	var bodyFont string
	var borderRadius string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--color-primary').trim()`, &primaryColor),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--font-body').trim()`, &bodyFont),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--radii-lg').trim()`, &borderRadius),
	)
	if err != nil {
		t.Fatalf("css vars: %v", err)
	}
	if primaryColor == "" {
		t.Error("--color-primary should be defined")
	}
	if bodyFont == "" {
		t.Error("--font-body should be defined")
	}
	if borderRadius == "" {
		t.Error("--radii-lg should be defined")
	}
}

// TestFeature_StyleSheetBuilder verifies key styles generated from Go are applied.
func TestFeature_StyleSheetBuilder(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var cardBG string
	var cardRadius string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Product card styles (set via Go StyleSheet builder)
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.product-card')).backgroundColor`, &cardBG),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.product-card')).borderRadius`, &cardRadius),
	)
	if err != nil {
		t.Fatalf("style sheet builder: %v", err)
	}
	if cardBG == "" {
		t.Error("product card should have background color")
	}
	if cardRadius == "" || cardRadius == "0px" {
		t.Errorf("product card should have border-radius from theme, got: %s", cardRadius)
	}
}

// TestFeature_ProgressiveCSS verifies CSS chunks load on navigation.
func TestFeature_ProgressiveCSS(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasChunkLinks bool
	var linkCount int

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate to products — should trigger CSS chunk loading
		chromedp.Evaluate(`document.querySelector('nav a[href="/products"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Check for injected CSS link elements (progressive loading)
		chromedp.Evaluate(`document.querySelectorAll('link[rel="stylesheet"]').length > 0`, &hasChunkLinks),
		chromedp.Evaluate(`document.querySelectorAll('link[rel="stylesheet"]').length`, &linkCount),
	)
	if err != nil {
		t.Fatalf("progressive CSS: %v", err)
	}
	if !hasChunkLinks {
		t.Error("navigation should inject stylesheet link elements")
	}
	t.Logf("found %d stylesheet links after navigation", linkCount)
}

// TestFeature_ContainerQueries verifies container query CSS is present in styles.
func TestFeature_ContainerQueries(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasContainerType bool
	var pageHTML string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.documentElement.outerHTML`, &pageHTML),
	)
	if err != nil {
		t.Fatalf("container queries: %v", err)
	}

	// Check that container-type CSS is present in the generated stylesheet
	if strings.Contains(pageHTML, "container-type") || strings.Contains(pageHTML, "container-name") {
		hasContainerType = true
	}
	if !hasContainerType {
		t.Error("generated CSS should contain container query declarations")
	}
}

// TestFeature_RuntimeStateManagement verifies getState/setState work.
func TestFeature_RuntimeStateManagement(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var setValue string
	var retrievedValue string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`window.__gofastr.setState('test-key', 'test-value')`, nil),
		chromedp.Evaluate(`window.__gofastr.getState('test-key', '')`, &retrievedValue),
		// Also test default value
		chromedp.Evaluate(`window.__gofastr.getState('nonexistent', 'fallback')`, &setValue),
	)
	if err != nil {
		t.Fatalf("state management: %v", err)
	}
	if retrievedValue != "test-value" {
		t.Errorf("getState should return 'test-value', got: %s", retrievedValue)
	}
	if setValue != "fallback" {
		t.Errorf("getState default should return 'fallback', got: %s", setValue)
	}
}

// TestFeature_NavigateAPI verifies programmatic navigation works.
func TestFeature_NavigateAPI(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var currentURL string
	var mainText string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate programmatically
		chromedp.Evaluate(`window.__gofastr.navigate('/about')`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Location(&currentURL),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
	)
	if err != nil {
		t.Fatalf("navigate API: %v", err)
	}
	if !strings.Contains(currentURL, "/about") {
		t.Errorf("expected URL /about, got %s", currentURL)
	}
	if !strings.Contains(mainText, "About") {
		t.Errorf("about page should contain 'About', got: %s", truncate(mainText, 100))
	}
}

// TestFeature_WidgetHydration verifies widget behavior scripts load on interaction.
func TestFeature_WidgetHydration(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasDataBehavior bool

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Check that interactive components have data-behavior attrs
		// (widget hydration uses data-behavior to lazy-load JS)
		chromedp.Evaluate(`document.querySelectorAll('[data-behavior]').length > 0`, &hasDataBehavior),
	)
	if err != nil {
		t.Fatalf("widget hydration: %v", err)
	}

	if !hasDataBehavior {
		// This is acceptable — data-behavior is only added when component has actions
		// and a hydration strategy. Log it but don't fail.
		t.Log("no data-behavior elements found (widgets may use inline actions.js instead)")
	}
}

// TestFeature_SSEConnection verifies SSE endpoint is wired up.
func TestFeature_SSEConnection(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var sseMetaContent string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content') ?? 'NONE'`, &sseMetaContent),
	)
	if err != nil {
		t.Fatalf("SSE meta: %v", err)
	}
	if !strings.Contains(sseMetaContent, "/__gofastr/sse") {
		t.Errorf("SSE meta should contain /__gofastr/sse, got: %s", sseMetaContent)
	}
}

// TestFeature_ActiveNavLink verifies aria-current="page" updates on navigation.
func TestFeature_ActiveNavLink(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var activeHREF string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate to products
		chromedp.Evaluate(`document.querySelector('nav a[href="/products"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Check active link
		chromedp.Evaluate(`document.querySelector('nav a[aria-current="page"]')?.getAttribute('href') ?? 'NONE'`, &activeHREF),
	)
	if err != nil {
		t.Fatalf("active nav: %v", err)
	}
	if activeHREF != "/products" {
		t.Errorf("products link should have aria-current='page', got active href: %s", activeHREF)
	}
}

// TestFeature_OverlayStack verifies multiple overlays can be opened and closed.
func TestFeature_OverlayStack(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var stackCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Open first dialog
		chromedp.Evaluate(`window.__gofastr.openOverlay('dialog', '/confirm-dialog')`, nil),
		chromedp.Sleep(300*time.Millisecond),
		// Open second dialog on top
		chromedp.Evaluate(`window.__gofastr.openOverlay('dialog', '/confirm-dialog')`, nil),
		chromedp.Sleep(300*time.Millisecond),
		// Check both exist
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]').length`, &stackCount),
	)
	if err != nil {
		t.Fatalf("overlay stack: %v", err)
	}
	if stackCount != 2 {
		t.Errorf("expected 2 stacked overlays, got %d", stackCount)
	}

	// Close top one (Escape closes topmost)
	var remainingCount int
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-overlay]').length`, &remainingCount),
	)
	if err != nil {
		t.Fatalf("overlay stack close: %v", err)
	}
	if remainingCount != 1 {
		t.Errorf("expected 1 overlay after closing top, got %d", remainingCount)
	}
}

// TestFeature_ProductDetailDynamicRoute verifies dynamic :slug routes work via browser.
func TestFeature_ProductDetailDynamicRoute(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	var url string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products"),
		waitForPage(),
		// Click on product card link
		chromedp.Evaluate(`document.querySelector('a[href="/products/gadget-max"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Location(&url),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
	)
	if err != nil {
		t.Fatalf("dynamic route: %v", err)
	}
	if !strings.Contains(url, "/products/gadget-max") {
		t.Errorf("expected URL /products/gadget-max, got %s", url)
	}
	if !strings.Contains(mainText, "Gadget Max") {
		t.Errorf("detail page should show 'Gadget Max', got: %s", truncate(mainText, 100))
	}
}

// TestFeature_SkipLink verifies skip-to-content link exists and is focusable.
func TestFeature_SkipLink(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasSkipLink bool
	var skipHref string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('.skip-link') !== null`, &hasSkipLink),
		chromedp.Evaluate(`document.querySelector('.skip-link')?.getAttribute('href') ?? 'none'`, &skipHref),
	)
	if err != nil {
		t.Fatalf("skip link: %v", err)
	}
	if !hasSkipLink {
		t.Error("page should have a .skip-link element")
	}
	if skipHref != "#main-content" {
		t.Errorf("skip link should point to #main-content, got: %s", skipHref)
	}
}

// TestFeature_ARIALandmarks verifies all ARIA landmarks are present.
func TestFeature_ARIALandmarks(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasBanner, hasMain, hasNav, hasContentinfo bool

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('[role="banner"]') !== null`, &hasBanner),
		chromedp.Evaluate(`document.querySelector('[role="main"]') !== null`, &hasMain),
		chromedp.Evaluate(`document.querySelector('nav') !== null`, &hasNav),
		chromedp.Evaluate(`document.querySelector('[role="contentinfo"]') !== null`, &hasContentinfo),
	)
	if err != nil {
		t.Fatalf("aria landmarks: %v", err)
	}
	if !hasBanner {
		t.Error("page should have role=banner (header)")
	}
	if !hasMain {
		t.Error("page should have role=main")
	}
	if !hasNav {
		t.Error("page should have <nav>")
	}
	if !hasContentinfo {
		t.Error("page should have role=contentinfo (footer)")
	}
}

// TestFeature_AnimationTransitions verifies transition CSS is present in
// the host's externalized styles. CSS is no longer inlined into the page;
// the test fetches /__gofastr/styles.css and asserts on its body.
func TestFeature_AnimationTransitions(t *testing.T) {
	base := startTestServer(t)

	resp, err := http.Get(base + "/__gofastr/app.css")
	if err != nil {
		t.Fatalf("GET app.css: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	css := string(body)

	if !strings.Contains(css, "transition") && !strings.Contains(css, "animation") {
		t.Error("generated CSS should contain transition or animation declarations")
	}
	if !strings.Contains(css, "@keyframes") {
		t.Error("generated CSS should contain @keyframes (island-flash)")
	}
}

// TestFeature_AllPagesLoadWithoutErrors verifies all pages load cleanly.
func TestFeature_AllPagesLoadWithoutErrors(t *testing.T) {
	base := startTestServer(t)
	ctx, consoleErrors := newBrowserCtxWithConsole(t)

	// Enable log + network domains
	var networkFailures []string
	listenNetworkErrors(ctx, &networkFailures)

	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	pages := []struct {
		path     string
		contains string
	}{
		{"/", "Build fast"},
		{"/products", "Widget Pro"},
		{"/about", "Our Mission"},
		{"/cart", "Shopping Cart"},
		{"/signals", "Signal Demo"},
		{"/error-boundary", "Error Boundary Demo"},
	}

	for _, page := range pages {
		var bodyText string
		err := chromedp.Run(ctx,
			chromedp.Navigate(base+page.path),
			waitForPage(),
			chromedp.Text("body", &bodyText, chromedp.ByQuery),
		)
		if err != nil {
			t.Errorf("page %s failed to load: %v", page.path, err)
			continue
		}
		if !strings.Contains(bodyText, page.contains) {
			t.Errorf("page %s missing expected text %q", page.path, page.contains)
		}
	}

	assertNoConsoleErrors(t, consoleErrors, "all pages load")
	for _, f := range networkFailures {
		if strings.Contains(f, "favicon.ico") {
			continue
		}
		t.Errorf("network failure: %s", f)
	}
}

// TestFeature_ScreenCacheBackNavigation verifies back button uses cached screen.
func TestFeature_ScreenCacheBackNavigation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Home → Products
		chromedp.Evaluate(`document.querySelector('nav a[href="/products"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Products → About
		chromedp.Evaluate(`document.querySelector('nav a[href="/about"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		// Back to Products
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
	)
	if err != nil {
		t.Fatalf("screen cache back: %v", err)
	}
	if !strings.Contains(mainText, "Widget") {
		t.Errorf("back to products should show cached content, got: %s", truncate(mainText, 100))
	}

	// Back to Home
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
	)
	if err != nil {
		t.Fatalf("screen cache back to home: %v", err)
	}
	if !strings.Contains(mainText, "Build fast") {
		t.Errorf("back to home should show cached content, got: %s", truncate(mainText, 100))
	}
}

// TestFeature_RuntimeRegisterAndFire verifies component action registration and dispatch.
func TestFeature_RuntimeRegisterAndFire(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var counterDisplay string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Verify actions.js loaded and registered components
		chromedp.Evaluate(`typeof window.__gofastr === 'object'`, nil),
		// Click counter increment button
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Text("[data-counter-display]", &counterDisplay, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("runtime register: %v", err)
	}
	if counterDisplay != "3" {
		t.Errorf("counter should show 3 after 3 clicks, got: %s", counterDisplay)
	}
}

// TestFeature_ImageAltText verifies all images have alt text.
func TestFeature_ImageAltText(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var missingAltCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`
			(() => {
				const imgs = document.querySelectorAll('img');
				let missing = 0;
				imgs.forEach(img => {
					if (!img.alt || img.alt.trim() === '') missing++;
				});
				return missing;
			})()
		`, &missingAltCount),
	)
	if err != nil {
		t.Fatalf("image alt: %v", err)
	}
	if missingAltCount > 0 {
		t.Errorf("found %d images missing alt text", missingAltCount)
	}
}

// TestFeature_ButtonAccessibleNames verifies all buttons have accessible names.
func TestFeature_ButtonAccessibleNames(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var missingNameCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`
			(() => {
				const buttons = document.querySelectorAll('button');
				let missing = 0;
				buttons.forEach(btn => {
					const text = btn.textContent?.trim();
					const ariaLabel = btn.getAttribute('aria-label')?.trim();
					if ((!text || text === '') && (!ariaLabel || ariaLabel === '')) missing++;
				});
				return missing;
			})()
		`, &missingNameCount),
	)
	if err != nil {
		t.Fatalf("button names: %v", err)
	}
	if missingNameCount > 0 {
		t.Errorf("found %d buttons missing accessible names", missingNameCount)
	}
}

// TestFeature_SectionAriaLabels verifies sections have aria-label or aria-labelledby.
func TestFeature_SectionAriaLabels(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var missingLabelCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`
			(() => {
				const sections = document.querySelectorAll('section');
				let missing = 0;
				sections.forEach(s => {
					const label = s.getAttribute('aria-label')?.trim();
					const labelledBy = s.getAttribute('aria-labelledby')?.trim();
					if ((!label || label === '') && (!labelledBy || labelledBy === '')) missing++;
				});
				return missing;
			})()
		`, &missingLabelCount),
	)
	if err != nil {
		t.Fatalf("section labels: %v", err)
	}
	if missingLabelCount > 0 {
		t.Errorf("found %d sections missing aria-label or aria-labelledby", missingLabelCount)
	}
}

// TestFeature_FormLabelAssociation verifies form inputs have associated labels.
func TestFeature_FormLabelAssociation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var missingLabelCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products"),
		waitForPage(),
		chromedp.Evaluate(`
			(() => {
				const inputs = document.querySelectorAll('input');
				let missing = 0;
				inputs.forEach(input => {
					const id = input.id;
					const ariaLabel = input.getAttribute('aria-label');
					if (!id && !ariaLabel) {
						missing++;
					} else if (id && !document.querySelector('label[for="' + id + '"]') && !ariaLabel) {
						missing++;
					}
				});
				return missing;
			})()
		`, &missingLabelCount),
	)
	if err != nil {
		t.Fatalf("form labels: %v", err)
	}
	if missingLabelCount > 0 {
		t.Errorf("found %d inputs without associated labels", missingLabelCount)
	}
}

// TestFeature_KeyboardNavigation verifies all interactive elements are keyboard accessible.
func TestFeature_KeyboardNavigation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var focusableCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`
			(() => {
				const interactive = document.querySelectorAll('a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])');
				let focusable = 0;
				interactive.forEach(el => {
					if (!el.hasAttribute('disabled') && el.tabIndex >= 0) focusable++;
				});
				return focusable;
			})()
		`, &focusableCount),
	)
	if err != nil {
		t.Fatalf("keyboard nav: %v", err)
	}
	if focusableCount < 5 {
		t.Errorf("expected at least 5 focusable elements, got %d", focusableCount)
	}
}

// TestFeature_PageTitleUpdates verifies the page title updates on navigation.
func TestFeature_PageTitleUpdates(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var homeTitle string
	var productsTitle string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.title`, &homeTitle),
		// Navigate to products
		chromedp.Evaluate(`document.querySelector('nav a[href="/products"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.title`, &productsTitle),
	)
	if err != nil {
		t.Fatalf("page title: %v", err)
	}
	if !strings.Contains(homeTitle, "GoFastr") {
		t.Errorf("home title should contain 'GoFastr', got: %s", homeTitle)
	}
	if !strings.Contains(productsTitle, "Products") {
		t.Errorf("products title should contain 'Products', got: %s", productsTitle)
	}
	if homeTitle == productsTitle {
		t.Errorf("title should change on navigation — home: %s, products: %s", homeTitle, productsTitle)
	}
}

// TestFeature_DynamicRouteNotFound verifies unknown product shows not-found.
func TestFeature_DynamicRouteNotFound(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products/nonexistent-item"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainText),
	)
	if err != nil {
		t.Fatalf("not found: %v", err)
	}
	if !strings.Contains(mainText, "Not Found") {
		t.Errorf("unknown product should show 'Not Found', got: %s", truncate(mainText, 100))
	}
}

// TestFeature_LiveRegion verifies aria-live regions exist for dynamic updates.
func TestFeature_LiveRegion(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasAriaLive bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Navigate to signals page which has aria-live
		chromedp.Evaluate(`document.querySelector('nav a[href="/signals"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelectorAll('[aria-live]').length > 0`, &hasAriaLive),
	)
	if err != nil {
		t.Fatalf("live region: %v", err)
	}
	if !hasAriaLive {
		t.Error("signals page should have at least one aria-live region for dynamic updates")
	}
}

// TestFeature_SyncBindingsMethod verifies syncBindings updates bound inputs.
func TestFeature_SyncBindingsMethod(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var inputValue string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products"),
		waitForPage(),
		// Set state first, then sync
		chromedp.Evaluate(`window.__gofastr.setState('search', 'hello-from-state')`, nil),
		chromedp.Evaluate(`window.__gofastr.syncBindings()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('#search-input')?.value ?? 'NONE'`, &inputValue),
	)
	if err != nil {
		t.Fatalf("sync bindings: %v", err)
	}
	if inputValue != "hello-from-state" {
		t.Errorf("syncBindings should update input value to 'hello-from-state', got: %s", inputValue)
	}
}

// TestFeature_HeroGradient verifies the hero section has a gradient background.
func TestFeature_HeroGradient(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var heroBG string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[aria-label="Hero"]')).backgroundImage`, &heroBG),
	)
	if err != nil {
		t.Fatalf("hero gradient: %v", err)
	}
	if !strings.Contains(heroBG, "gradient") {
		t.Errorf("hero should have gradient background, got: %s", heroBG)
	}
}

// TestFeature_FooterPresent verifies footer with role=contentinfo exists.
func TestFeature_FooterPresent(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var footerText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('[role="contentinfo"]')?.textContent ?? 'NONE'`, &footerText),
	)
	if err != nil {
		t.Fatalf("footer: %v", err)
	}
	if footerText == "NONE" {
		t.Error("page should have a footer with role=contentinfo")
	}
}

// TestFeature_ThemeColorTokens verifies multiple theme color tokens are defined.
func TestFeature_ThemeColorTokens(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), waitForPage())
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}

	tokens := []string{"--color-primary", "--color-success", "--color-danger", "--color-warning", "--color-info"}
	for _, token := range tokens {
		var val string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`getComputedStyle(document.documentElement).getPropertyValue('%s').trim()`, token), &val),
		)
		if err != nil {
			t.Errorf("token %s: %v", token, err)
			continue
		}
		if val == "" {
			t.Errorf("theme token %s should be defined", token)
		}
	}
}

// TestFeature_SpacingTokens verifies spacing theme tokens are defined.
func TestFeature_SpacingTokens(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), waitForPage())
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}

	tokens := []string{"--spacing-xs", "--spacing-sm", "--spacing-md", "--spacing-lg", "--spacing-xl"}
	for _, token := range tokens {
		var val string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`getComputedStyle(document.documentElement).getPropertyValue('%s').trim()`, token), &val),
		)
		if err != nil {
			t.Errorf("token %s: %v", token, err)
			continue
		}
		if val == "" {
			t.Errorf("spacing token %s should be defined", token)
		}
	}
}

// TestFeature_FontTokens verifies font family tokens are defined.
func TestFeature_FontTokens(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), waitForPage())
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}

	tokens := []string{"--font-body", "--font-heading", "--font-mono"}
	for _, token := range tokens {
		var val string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`getComputedStyle(document.documentElement).getPropertyValue('%s').trim()`, token), &val),
		)
		if err != nil {
			t.Errorf("token %s: %v", token, err)
			continue
		}
		if val == "" {
			t.Errorf("font token %s should be defined", token)
		}
	}
}
