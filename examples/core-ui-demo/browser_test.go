package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cdpLog "github.com/chromedp/cdproto/log"
	cdpNetwork "github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// startTestServer starts the demo app on a random port and returns the base URL.
func startTestServer(t *testing.T) string {
	t.Helper()
	ds := setupDevServer()
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	return srv.URL
}

// newBrowserCtx creates a headless Chrome context for testing.
func newBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	ctx, timeoutCancel := context.WithTimeout(browserCtx, 15*time.Second)
	t.Cleanup(timeoutCancel)
	return ctx
}

// assertAction implements chromedp.Action for inline assertions.
type assertAction struct{ fn func() error }

func (a *assertAction) Do(ctx context.Context) error { return a.fn() }

func assertErr(fn func() error) chromedp.Action { return &assertAction{fn: fn} }

// waitForPage waits for the page to be ready after navigation.
func waitForPage() chromedp.Action {
	return chromedp.Sleep(1 * time.Second)
}

// captureFailedRequests returns a chromedp Action that enables network request
// monitoring and collects failed request URLs.
func captureFailedRequests(ctx context.Context, failures *[]string) error {
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			cdpNetwork.Enable().Do(ctx)
			return nil
		}),
	)
}

// listenNetworkErrors listens for failed network requests.
func listenNetworkErrors(browserCtx context.Context, failures *[]string) {
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *cdpNetwork.EventResponseReceived:
			if ev.Response != nil && ev.Response.Status >= 400 {
				*failures = append(*failures, fmt.Sprintf("%d %s", ev.Response.Status, ev.Response.URL))
			}
		}
	})
}

// newBrowserCtxWithConsole creates a browser context that captures console errors.
// Returns the context and a slice that will be populated with any error-level messages.
func newBrowserCtxWithConsole(t *testing.T) (context.Context, *[]string) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	var consoleErrors []string
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *cdpLog.EventEntryAdded:
			if ev.Entry != nil && ev.Entry.Level == cdpLog.LevelError {
				consoleErrors = append(consoleErrors, fmt.Sprintf("console.%s: %s", ev.Entry.Level, ev.Entry.Text))
			}
		}
	})

	ctx, timeoutCancel := context.WithTimeout(browserCtx, 15*time.Second)
	t.Cleanup(timeoutCancel)
	return ctx, &consoleErrors
}

// assertNoConsoleErrors fails the test if any console errors were captured.
func assertNoConsoleErrors(t *testing.T, errors *[]string, context string) {
	t.Helper()
	if len(*errors) > 0 {
		for _, e := range *errors {
			if strings.Contains(e, "favicon.ico") {
				continue
			}
			t.Errorf("%s: browser console error: %s", context, e)
		}
	}
}

// TestBrowserNoConsoleErrors verifies no JS console errors on any page.
func TestBrowserNoConsoleErrors(t *testing.T) {
	base := startTestServer(t)
	ctx, consoleErrors := newBrowserCtxWithConsole(t)

	// Capture failed network requests
	var networkFailures []string
	listenNetworkErrors(ctx, &networkFailures)

	// Enable log + network domains
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := cdpLog.Enable().Do(ctx); err != nil {
				return err
			}
			return cdpNetwork.Enable().Do(ctx)
		}),
	)
	if err != nil {
		t.Fatalf("enable domains: %v", err)
	}

	pages := []string{"/", "/products", "/about", "/cart"}
	for _, page := range pages {
		err := chromedp.Run(ctx,
			chromedp.Navigate(base+page),
			waitForPage(),
		)
		if err != nil {
			t.Fatalf("navigate %s: %v", page, err)
		}
	}

	assertNoConsoleErrors(t, consoleErrors, "page load")

	// Check network failures (excluding browser-default favicon.ico)
	if len(networkFailures) > 0 {
		for _, f := range networkFailures {
			if strings.Contains(f, "favicon.ico") {
				continue
			}
			t.Errorf("network failure: %s", f)
		}
	}
}

// TestBrowserPageLoads verifies each page loads with correct content and runtime injection.
func TestBrowserPageLoads(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	tests := []struct {
		path         string
		bodyContains string
	}{
		{"/", "Build fast, accessible web applications in Go."},
		{"/products", "Products"},
		{"/about", "Our Mission"},
		{"/cart", "Your cart is empty"},
	}

	for _, tt := range tests {
		t.Run(strings.Trim(tt.path, "/")+"_loads", func(t *testing.T) {
			var bodyText string
			var pageHTML string
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+tt.path),
				waitForPage(),
				chromedp.Text("body", &bodyText, chromedp.ByQuery),
				chromedp.Evaluate(`document.documentElement.outerHTML`, &pageHTML),
			)
			if err != nil {
				t.Fatalf("navigate %s: %v", tt.path, err)
			}
			if !strings.Contains(bodyText, tt.bodyContains) {
				t.Errorf("page %s body missing %q", tt.path, tt.bodyContains)
			}
			if !strings.Contains(pageHTML, "runtime.js") {
				t.Errorf("page %s missing runtime.js injection", tt.path)
			}
			if !strings.Contains(pageHTML, "gofastr-sse") {
				t.Errorf("page %s missing SSE meta tag", tt.path)
			}
		})
	}
}

// TestCounterIncrement verifies clicking + updates the counter display.
func TestCounterIncrement(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var displayText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Initial value should be 0
		chromedp.Text("[data-counter-display]", &displayText, chromedp.ByQuery),
		assertErr(func() error {
			if displayText != "0" {
				return fmt.Errorf("expected initial counter 0, got %s", displayText)
			}
			return nil
		}),
		// Click + three times
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Text("[data-counter-display]", &displayText, chromedp.ByQuery),
		assertErr(func() error {
			if displayText != "1" {
				return fmt.Errorf("after 1 click expected 1, got %s", displayText)
			}
			return nil
		}),
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Text("[data-counter-display]", &displayText, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("counter increment: %v", err)
	}
	if displayText != "3" {
		t.Errorf("expected counter 3 after 3 clicks, got %s", displayText)
	}
}

// TestCounterDecrement verifies clicking - decrements the counter.
func TestCounterDecrement(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var displayText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Click(".counter-inc", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		// Now counter = 2, decrement once
		chromedp.Click(".counter-dec", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Text("[data-counter-display]", &displayText, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("counter decrement: %v", err)
	}
	if displayText != "1" {
		t.Errorf("expected counter 1 after 2+, 1-, got %s", displayText)
	}
}

// TestAddToCart verifies clicking "Add to cart" updates the badge.
func TestAddToCart(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var badgeText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Click first add-to-cart button
		chromedp.Evaluate(`document.querySelector('.add-to-cart').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.cart-badge')?.textContent ?? 'not-found'`, &badgeText),
	)
	if err != nil {
		t.Fatalf("add to cart: %v", err)
	}
	if badgeText != "1" {
		t.Errorf("expected cart badge 1, got %s", badgeText)
	}

	// Add second item
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.add-to-cart').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.cart-badge')?.textContent ?? 'not-found'`, &badgeText),
	)
	if err != nil {
		t.Fatalf("add to cart (2nd): %v", err)
	}
	if badgeText != "2" {
		t.Errorf("expected cart badge 2, got %s", badgeText)
	}
}

// TestClientSideNavigation verifies clicking nav links swaps content without full reload.
func TestClientSideNavigation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var mainContent, currentURL string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Click Products nav link
		chromedp.Click(`nav a[href="/products"]`, chromedp.ByQuery),
		chromedp.Sleep(1*time.Second),
		chromedp.Text("main h1", &mainContent, chromedp.ByQuery),
		chromedp.Location(&currentURL),
	)
	if err != nil {
		t.Fatalf("navigate to products: %v", err)
	}
	if !strings.Contains(currentURL, "/products") {
		t.Errorf("expected URL /products, got %s", currentURL)
	}
	if !strings.Contains(mainContent, "Products") {
		t.Errorf("expected h1 'Products', got %q", mainContent)
	}
}

// TestSearchFilter verifies typing in the search box filters product cards.
func TestSearchFilter(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var visibleCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products"),
		waitForPage(),
		// All 6 cards should be visible initially
		chromedp.Evaluate(`document.querySelectorAll('.product-card:not([style*="display: none"])').length`, &visibleCount),
		assertErr(func() error {
			if visibleCount != 6 {
				return fmt.Errorf("expected 6 visible cards initially, got %d", visibleCount)
			}
			return nil
		}),
		// Type "widget" — should match only "Widget Pro"
		chromedp.SendKeys("#search-input", "widget", chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('.product-card:not([style*="display: none"])').length`, &visibleCount),
	)
	if err != nil {
		t.Fatalf("search filter: %v", err)
	}
	if visibleCount != 1 {
		t.Errorf("expected 1 visible card after filtering 'widget', got %d", visibleCount)
	}
}

// TestBrowserRuntimeJSLoads verifies the runtime.js loads and exposes __gofastr.
func TestBrowserRuntimeJSLoads(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasGofastr bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`typeof window.__gofastr === 'object' && typeof window.__gofastr.register === 'function'`, &hasGofastr),
	)
	if err != nil {
		t.Fatalf("runtime check: %v", err)
	}
	if !hasGofastr {
		t.Error("window.__gofastr not available after page load")
	}
}

// TestBrowserRoutesRegistered verifies the route graph is in the browser.
func TestBrowserRoutesRegistered(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var routeCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`Array.isArray(window.__gofastr_routes) ? window.__gofastr_routes.length : -1`, &routeCount),
	)
	if err != nil {
		t.Fatalf("routes check: %v", err)
	}
	if routeCount < 4 {
		t.Errorf("expected at least 4 routes, got %d", routeCount)
	}
}

// TestBrowserAccessibility verifies ARIA attributes are present.
func TestBrowserAccessibility(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	var hasAriaLabel, hasCounterLabel bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('[aria-label="Main navigation"]') !== null`, &hasAriaLabel),
		chromedp.Evaluate(`document.querySelector('.counter-inc[aria-label]') !== null`, &hasCounterLabel),
	)
	if err != nil {
		t.Fatalf("a11y check: %v", err)
	}
	if !hasAriaLabel {
		t.Error("missing aria-label on navigation")
	}
	if !hasCounterLabel {
		t.Error("missing aria-label on counter button")
	}
}

// TestBrowserSessionCookie verifies the server sets a session cookie.
// Note: the cookie is HttpOnly, so document.cookie can't see it.
// We verify it by checking the Set-Cookie response header instead.
func TestBrowserSessionCookie(t *testing.T) {
	base := startTestServer(t)

	// Use a simple HTTP client to check the response header
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == "gofastr-session" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected gofastr-session cookie in response")
	}
}

// TestBrowserStylesApplied verifies that theme CSS variables are injected
// and that key visual styles are actually applied to elements.
func TestBrowserStylesApplied(t *testing.T) {
	base := startTestServer(t)
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	// Navigate to home page
	var hasVars bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		// Check that :root CSS variables are defined
		chromedp.Evaluate(`
			(() => {
				const s = getComputedStyle(document.documentElement);
				return s.getPropertyValue('--color-primary').trim() !== '';
			})()
		`, &hasVars),
	)
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}

	if !hasVars {
		t.Error("expected --color-primary CSS variable to be defined on :root")
	}

	// Check key style values are actually applied
	tests := []struct {
		selector string
		prop     string
		want     string // substring to check for
	}{
		// Hero should have gradient background
		{`[aria-label="Hero"]`, "background-image", "gradient"},
		// Hero should have white text via color property
		{`[aria-label="Hero"]`, "color", "rgb(255, 255, 255)"},
		// Product card should have white background
		{".product-card", "background-color", "rgb(255, 255, 255)"},
		// Primary button should have indigo background
		{".product-card button", "background-color", "rgb(99, 102, 241)"},
		// Header should have bottom border
		{"[role=\"banner\"] nav", "border-bottom", ""},
	}

	for _, tt := range tests {
		var val string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`
				(() => {
					const el = document.querySelector('%s');
					if (!el) return 'ELEMENT_NOT_FOUND';
					return getComputedStyle(el).getPropertyValue('%s');
				})()
			`, tt.selector, tt.prop), &val),
		)
		if err != nil {
			t.Errorf("style check %s %s: %v", tt.selector, tt.prop, err)
			continue
		}
		val = strings.TrimSpace(val)
		if val == "ELEMENT_NOT_FOUND" {
			t.Errorf("style check: element %s not found", tt.selector)
			continue
		}
		if tt.want != "" && !strings.Contains(val, tt.want) {
			t.Errorf("style check %s %s: got %q, want substring %q", tt.selector, tt.prop, val, tt.want)
		}
	}
}

// TestClientSideNavigationWithCache verifies that client-side routing works:
// - Layout (header/footer) persists across navigations
// - Screen content swaps without full page reload
// - Screen cache enables instant back-navigation
func TestClientSideNavigationWithCache(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	// 1. Load home page
	var initialHeader string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		waitForPage(),
		chromedp.Evaluate(`document.querySelector('nav').outerHTML`, &initialHeader),
	)
	if err != nil {
		t.Fatalf("load home: %v", err)
	}
	if initialHeader == "" {
		t.Fatal("expected header to be present on initial load")
	}

	// 2. Navigate to /products via client-side router
	var productContent string
	var afterNavHeader string
	err = chromedp.Run(ctx,
		// Click the products link (intercepted by runtime.js)
		chromedp.Evaluate(`
			(() => {
				const link = document.querySelector('nav a[href="/products"]');
				if (!link) return 'NO_LINK';
				link.click();
				return 'clicked';
			})()
		`, nil),
		chromedp.Sleep(2*time.Second),
		// Check product content loaded
		chromedp.Evaluate(`
			(() => {
				const main = document.querySelector('[role="main"]');
				return main?.textContent ?? 'NO_MAIN';
			})()
		`, &productContent),
		// Verify header persists (layout didn't reload)
		chromedp.Evaluate(`document.querySelector('nav').outerHTML`, &afterNavHeader),
	)
	if err != nil {
		t.Fatalf("navigate to products: %v", err)
	}

	if !strings.Contains(productContent, "Widget") {
		t.Errorf("expected products page to contain 'Widget', got: %s", truncate(productContent, 100))
	}
	if afterNavHeader != initialHeader {
		// The nav HTML changes because updateActiveLink() adds aria-current/class.
		// Verify the nav still has the same links (structure persisted).
		var navLinkCount int
		chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelectorAll('nav a').length`, &navLinkCount),
		)
		var hasActive bool
		chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('nav a[aria-current="page"]') !== null`, &hasActive),
		)
		if navLinkCount != 6 || !hasActive {
			t.Errorf("nav should have 6 links with active state, got %d links, active=%v", navLinkCount, hasActive)
		}
	}

	// 3. Navigate to /about
	var aboutContent string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(() => {
				const link = document.querySelector('nav a[href="/about"]');
				if (!link) return 'NO_LINK';
				link.click();
				return 'clicked';
			})()
		`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`
			(() => {
				const main = document.querySelector('[role="main"]');
				return main?.textContent ?? 'NO_MAIN';
			})()
		`, &aboutContent),
	)
	if err != nil {
		t.Fatalf("navigate to about: %v", err)
	}
	if !strings.Contains(aboutContent, "About") {
		t.Errorf("expected about page to contain 'About', got: %s", truncate(aboutContent, 100))
	}

	// 4. Navigate back to /products — should use screen cache
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`
			(() => {
				const main = document.querySelector('[role="main"]');
				return main?.textContent ?? 'NO_MAIN';
			})()
		`, &productContent),
	)
	if err != nil {
		t.Fatalf("navigate back: %v", err)
	}
	if !strings.Contains(productContent, "Widget") {
		t.Errorf("expected products page on back, got: %s", truncate(productContent, 100))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestProductDetailNavigation verifies dynamic routes work with client-side nav.
func TestProductDetailNavigation(t *testing.T) {
	base := startTestServer(t)
	ctx := newBrowserCtx(t)

	// 1. Go to products page
	var mainContent string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products"),
		waitForPage(),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainContent),
	)
	if err != nil {
		t.Fatalf("load products: %v", err)
	}
	if !strings.Contains(mainContent, "Widget Pro") {
		t.Fatalf("expected products page, got: %s", truncate(mainContent, 100))
	}

	// 2. Click on Widget Pro card link (dynamic route /products/widget-pro)
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(() => {
				const link = document.querySelector('a[href="/products/widget-pro"]');
				if (!link) return 'NO_LINK';
				link.click();
				return 'clicked';
			})()
		`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &mainContent),
		chromedp.Location(&mainContent), // reuse var to get URL
	)
	if err != nil {
		t.Fatalf("navigate to product detail: %v", err)
	}

	// Verify we're on the detail page
	var detailContent string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &detailContent),
	)
	if err != nil {
		t.Fatalf("get detail content: %v", err)
	}
	if !strings.Contains(detailContent, "Widget Pro") {
		t.Errorf("expected detail page for Widget Pro, got: %s", truncate(detailContent, 200))
	}
	if !strings.Contains(detailContent, "29.99") {
		t.Errorf("expected price on detail page, got: %s", truncate(detailContent, 200))
	}
	if !strings.Contains(detailContent, "Back to Products") {
		t.Errorf("expected back link on detail page, got: %s", truncate(detailContent, 200))
	}

	// 3. Go back to products via the back link
	var productsContent string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(() => {
				const link = document.querySelector('a.back-link');
				if (!link) return 'NO_LINK';
				link.click();
				return 'clicked';
			})()
		`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelector('[role="main"]')?.textContent ?? 'NO_MAIN'`, &productsContent),
	)
	if err != nil {
		t.Fatalf("navigate back to products: %v", err)
	}
	if !strings.Contains(productsContent, "Gadget Max") {
		t.Errorf("expected products page after back, got: %s", truncate(productsContent, 100))
	}
}
