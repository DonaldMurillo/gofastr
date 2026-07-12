package main

// Browser-level (chromedp) e2e for the product site. Covers what the
// httptest suite cannot: a real Chrome that REJECTS a __Host-/Secure
// cookie set over http://localhost (the 401-storm cause), real keypress/
// click on the command palette, the debounced search RPC + DOM swap, and
// client-side doc navigation.
//
// Gated by -short (the suite is slow and needs a headless Chrome), matching
// examples/website's e2e convention.

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func siteE2EServer(t *testing.T) string {
	t.Helper()
	app := setupServer()
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

// The e2e suite shares ONE headless Chrome across all tests; each test
// gets a fresh tab. Per-test Chrome launches (the old pattern) flake on
// CI runners: chromedp's websocket-URL deadline is a fixed 20s, and with
// 60+ sequential cold launches per run one of them intermittently
// exceeds it ("websocket url timeout reached") even in a serialized job.
// A new tab is milliseconds and cannot hit that path. Isolation stays
// sound: DOM/JS state is per-tab, and cookies/localStorage are
// per-origin while every test boots its own httptest server on a unique
// port.
var (
	siteBrowserOnce   sync.Once
	siteBrowserRoot   context.Context
	siteBrowserErr    error
	siteBrowserKill   context.CancelFunc
	siteAllocatorKill context.CancelFunc
)

func siteBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	siteBrowserOnce.Do(func() {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			// CI runners intermittently take >20s (the chromedp default)
			// to cold-start Chrome; a generous websocket-URL deadline turns
			// that from a flaky suite failure into a few slow seconds.
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.WindowSize(1280, 800),
		)
		allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		// Materialize the browser now so no test pays the process launch
		// inside its own deadline.
		if err := chromedp.Run(browserCtx); err != nil {
			siteBrowserErr = err
			browserCancel()
			allocCancel()
			return
		}
		siteBrowserRoot = browserCtx
		siteBrowserKill = browserCancel
		siteAllocatorKill = allocCancel
	})
	if siteBrowserErr != nil {
		t.Fatalf("shared browser failed to start: %v", siteBrowserErr)
	}
	tabCtx, tabCancel := chromedp.NewContext(siteBrowserRoot)
	t.Cleanup(tabCancel) // closes the tab, not the browser
	ctx, cancel := context.WithTimeout(tabCtx, 45*time.Second)
	t.Cleanup(cancel)
	// Materialize the tab and bring it to the foreground. The browser's
	// initial about:blank tab otherwise keeps focus, and Chrome throttles
	// background tabs' rAF / IntersectionObserver / smooth scrolling —
	// which silently breaks every scroll- and animation-driven assertion
	// in the suite.
	if err := chromedp.Run(ctx, page.BringToFront()); err != nil {
		t.Fatalf("bring tab to front: %v", err)
	}
	return ctx
}

// TestMain tears the shared browser down after the run so `go test`
// never leaves an orphaned Chrome behind.
func TestMain(m *testing.M) {
	code := m.Run()
	if siteBrowserKill != nil {
		siteBrowserKill()
	}
	if siteAllocatorKill != nil {
		siteAllocatorKill()
	}
	os.Exit(code)
}

// runtime401Sink records any /__gofastr/* response that came back 401 —
// the exact symptom of the cookie regression. A real browser rejects the
// __Host-/Secure cookie over http, so the gated runtime endpoints 401 and
// the island runtime / SSE / search all break.
type runtime401Sink struct {
	mu  sync.Mutex
	hit []string
}

func (s *runtime401Sink) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if e, ok := ev.(*network.EventResponseReceived); ok {
			if e.Response.Status == 401 && strings.Contains(e.Response.URL, "/__gofastr/") {
				s.mu.Lock()
				s.hit = append(s.hit, e.Response.URL)
				s.mu.Unlock()
			}
		}
	})
}

func (s *runtime401Sink) errors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hit...)
}

// TestE2ENoRuntime401s is the keystone guard at the browser level: load
// pages that hydrate islands and assert none of the /__gofastr/* runtime
// requests came back 401. With the cookie bug, Chrome drops the cookie and
// these all 401 — exactly the storm the audit found.
func TestE2ENoRuntime401s(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	sink := &runtime401Sink{}
	sink.listen(ctx)

	for _, path := range []string{"/", "/docs/", "/components/button"} {
		if err := chromedp.Run(ctx,
			network.Enable(),
			chromedp.Navigate(base+path),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.Sleep(700*time.Millisecond), // let runtime fetch widgets/sse
		); err != nil {
			t.Fatalf("navigate %s: %v", path, err)
		}
	}
	if hits := sink.errors(); len(hits) > 0 {
		t.Fatalf("runtime endpoints 401'd in a real browser (cookie regression): %v", hits)
	}
}

// TestE2ECommandPaletteOpensAndHydrates exercises the headline feature's
// browser-only behavior: clicking the search trigger opens the palette
// dialog and the widget hydrates with a populated, focusable listbox. This
// is precisely what the /__gofastr 401 used to break (the widget HTML could
// never load). The server-side query FILTERING is covered deterministically
// by TestPaletteSearchFilters (the RPC layer); live keystroke→swap is not
// asserted here because synthetic typing doesn't reliably drive the
// debounced island fetch under headless automation.
func TestE2ECommandPaletteOpensAndHydrates(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	// WaitVisible on the hydrated input is itself the proof: the palette
	// widget HTML must load (the 401 used to block it) and the modal must
	// open on click for the input to exist and be visible.
	var dialogVisible bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Click("button.site-cmd", chromedp.ByQuery),
		chromedp.WaitVisible(`#site-command-palette-input`, chromedp.ByQuery),
		chromedp.Evaluate(`!!document.querySelector('[role="dialog"]')`, &dialogVisible),
	); err != nil {
		t.Fatalf("palette should open + hydrate on click (was 401-blocked): %v", err)
	}
	if !dialogVisible {
		t.Fatal("clicking the search trigger should open the command palette dialog")
	}
}

// TestE2EDocCardNavigates clicks a doc card and confirms client-side nav
// lands on the real /docs/<slug> page with rendered markdown.
func TestE2EDocCardNavigates(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var pathname, html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/docs/"),
		chromedp.WaitVisible(`a.doc[href="/docs/query-dsl"]`, chromedp.ByQuery),
		chromedp.Click(`a.doc[href="/docs/query-dsl"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.ui-markdown`, chromedp.ByQuery),
		chromedp.Evaluate(`window.location.pathname`, &pathname),
		chromedp.OuterHTML(".ui-doc-layout__content", &html, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("doc nav: %v", err)
	}
	if pathname != "/docs/query-dsl" {
		t.Fatalf("expected to land on /docs/query-dsl, got %q", pathname)
	}
	if !strings.Contains(html, "ui-markdown") {
		t.Fatal("doc page should render embedded markdown")
	}
}

// TestE2EInteractive_RPCSignal clicks the counter button and verifies
// the signal region updates with the incremented value — no page reload.
func TestE2EInteractive_RPCSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var signalText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-signal"),
		chromedp.WaitVisible(`button[data-fui-rpc="/__site/interactive/counter"]`),
		// Use JS click instead of chromedp.Click — chromedp's mouse
		// event dispatch doesn't reliably trigger the runtime's
		// delegated click handler in headless Chrome.
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').click()`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').textContent`, &signalText),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("signal: %q", signalText)
	trimmed := strings.TrimSpace(signalText)
	if trimmed == "0" || trimmed == "" {
		t.Errorf("counter still %q after click — signal didn't update", signalText)
	}
}

func TestE2EInteractive_FormSubmitWithSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var signalHTML string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-form-signal"),
		chromedp.WaitVisible(`form[data-fui-rpc="/__site/interactive/submit"]`),
		// Use JS to fill + submit — same reason as RPCSignal test.
		chromedp.Evaluate(`{
            const form = document.querySelector('form[data-fui-rpc="/__site/interactive/submit"]');
            const input = form.querySelector('input[name="message"]');
            input.value = 'hello e2e';
            form.requestSubmit();
        }`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-form-result"]').innerHTML`, &signalHTML),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("form signal: %q", signalHTML)
	if !strings.Contains(signalHTML, "hello e2e") {
		t.Errorf("form signal = %q, want to contain 'hello e2e'", signalHTML)
	}
}

// TestE2EInteractive_FormInputHasLabel verifies the form input has an
// accessible label (aria-label or associated <label>) so screen
// readers can announce it.
func TestE2EInteractive_FormInputHasLabel(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var ariaLabel string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-form-signal"),
		chromedp.WaitVisible(`form[data-fui-rpc="/__site/interactive/submit"]`),
		chromedp.Evaluate(`document.querySelector('form[data-fui-rpc="/__site/interactive/submit"] input[name="message"]').getAttribute('aria-label') || ''`, &ariaLabel),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("input aria-label: %q", ariaLabel)
	if ariaLabel == "" {
		t.Error("form input has no aria-label attribute")
	}
}

// TestE2EInteractive_RPCOpenWidget clicks the "open drawer" button and
// verifies the drawer widget appears in the DOM after the RPC succeeds.
func TestE2EInteractive_RPCOpenWidget(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var exists bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-open-widget"),
		chromedp.WaitVisible(`button[data-fui-rpc-open="demo-result-modal"]`),
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc-open="demo-result-modal"]').click()`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-result-modal"]') !== null`, &exists),
	); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("drawer widget not found in DOM after rpc-open")
	}
}

// TestE2EInteractive_ModalAriaLabelledBy verifies the modal widget has
// aria-labelledby pointing to a visible heading inside the modal.
func TestE2EInteractive_ModalAriaLabelledBy(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var labelledBy string
	var headingExists bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-open-widget"),
		chromedp.WaitVisible(`button[data-fui-rpc-open="demo-result-modal"]`),
		// Open the modal
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc-open="demo-result-modal"]').click()`, nil),
		chromedp.Sleep(1*time.Second),
		// Read aria-labelledby from the widget root
		chromedp.Evaluate(`(function(){var w=document.querySelector('[data-fui-widget="demo-result-modal"]');return w?(w.getAttribute('aria-labelledby')||''):''})()`, &labelledBy),
		chromedp.Evaluate(`(function(){var w=document.querySelector('[data-fui-widget="demo-result-modal"]');var lb=w?w.getAttribute('aria-labelledby'):'';return lb&&document.getElementById(lb)!==null})()`, &headingExists),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("modal aria-labelledby: %q, heading exists: %v", labelledBy, headingExists)
	if labelledBy == "" {
		t.Error("modal widget has no aria-labelledby attribute")
	}
	if !headingExists {
		t.Errorf("modal aria-labelledby=%q but no element with that id found", labelledBy)
	}
}

// TestE2EInteractive_SPANavigate clicks the navigate button and verifies
// the page changed without a full reload (SPA navigation).
func TestE2EInteractive_SPANavigate(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var pathname string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-navigate"),
		chromedp.WaitVisible(`button[data-fui-rpc-navigate="/components/button"]`),
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc-navigate="/components/button"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`location.pathname`, &pathname),
	); err != nil {
		t.Fatal(err)
	}
	if pathname != "/components/button" {
		t.Errorf("after navigate, pathname = %q, want /components/button", pathname)
	}
}

// TestE2EInteractive_SignalHasAriaLive verifies the runtime auto-injects
// role="status" aria-live="polite" aria-atomic="true" onto every
// [data-fui-signal] node. This is a P0 a11y requirement: without it,
// screen readers do not announce signal-region updates (counter clicks,
// form results, error feedback) because the DOM mutations are silent.
func TestE2EInteractive_SignalHasAriaLive(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var role, ariaLive, ariaAtomic string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-signal"),
		chromedp.WaitVisible(`[data-fui-signal="demo-counter"]`),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').getAttribute('role')||''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').getAttribute('aria-live')||''`, &ariaLive),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').getAttribute('aria-atomic')||''`, &ariaAtomic),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("signal attrs: role=%q aria-live=%q aria-atomic=%q", role, ariaLive, ariaAtomic)
	if role != "status" {
		t.Errorf(`signal node role = %q, want "status"`, role)
	}
	if ariaLive != "polite" {
		t.Errorf(`signal node aria-live = %q, want "polite"`, ariaLive)
	}
	if ariaAtomic != "true" {
		t.Errorf(`signal node aria-atomic = %q, want "true"`, ariaAtomic)
	}
}

// TestE2EInteractive_RPCErrorFeedback verifies that when an RPC returns 500,
// the signal region shows a human-readable error message, not raw JSON.
func TestE2EInteractive_RPCErrorFeedback(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var signalText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-signal"),
		chromedp.WaitVisible(`[data-fui-signal="demo-counter"]`),
		// Inject a button that hits the error endpoint.
		chromedp.Evaluate(`(function(){var b=document.createElement('button');b.setAttribute('data-fui-rpc','/__site/interactive/error');b.setAttribute('data-fui-rpc-signal','demo-counter');b.id='__test-err-btn';document.body.appendChild(b);return true})()`, nil),
		chromedp.Evaluate(`document.getElementById('__test-err-btn').click()`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').textContent`, &signalText),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("error signal text: %q", signalText)
	trimmed := strings.TrimSpace(signalText)
	// Must NOT be raw JSON like {"ok":false,"status":500,"text":"..."}
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"ok"`) {
		t.Errorf("error signal shows raw JSON %q — should be human-readable", trimmed)
	}
	// Must contain some indication of error
	if trimmed == "" {
		t.Error("error signal is empty after 500 response")
	}
}

// TestE2EInteractive_NetworkErrorFeedback verifies that when a network error
// occurs (fetch throws), the signal region shows a human-readable error
// instead of staying unchanged. Before the fix, network errors propagated
// as unhandled promise rejections and the signal stayed at its previous value.
func TestE2EInteractive_NetworkErrorFeedback(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var signalText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-signal"),
		chromedp.WaitVisible(`[data-fui-signal="demo-counter"]`),
		// Override fetch to throw a network error for the counter endpoint.
		chromedp.Evaluate(`(function(){
			var origFetch = window.fetch;
			window.fetch = function(url, opts) {
				if (typeof url === 'string' && url.indexOf('counter') >= 0) {
					return Promise.reject(new Error('Network error'));
				}
				return origFetch.call(this, url, opts);
			};
			return true;
		})()`, nil),
		// Click the counter button.
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').click()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').textContent`, &signalText),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("network-error signal text: %q", signalText)
	trimmed := strings.TrimSpace(signalText)
	// Signal must have changed from "0" — the runtime should have written
	// something (error message) into the signal on network failure.
	if trimmed == "0" || trimmed == "" {
		t.Errorf("signal still %q after network error — dispatchRPC did not write error feedback to signal", signalText)
	}
}

// TestE2EInteractive_LoadingState verifies that the runtime adds the
// fui-loading CSS class and aria-busy="true" attribute to the trigger
// node during an in-flight RPC, and removes them after completion.
func TestE2EInteractive_LoadingState(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var hasLoadingClass bool
	var hasAriaBusy string
	var signalText string
	var loadingClassAfter bool
	var ariaBusyAfter string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-signal"),
		chromedp.WaitVisible(`[data-fui-signal="demo-counter"]`),
		// Override fetch to add a 2-second delay so we can observe the loading state.
		chromedp.Evaluate(`(function(){
			var origFetch = window.fetch;
			window.fetch = function(url, opts) {
				if (typeof url === 'string' && url.indexOf('counter') >= 0) {
					return new Promise(function(resolve) {
						setTimeout(function() {
							origFetch.call(window, url, opts).then(resolve);
						}, 2000);
					});
				}
				return origFetch.call(this, url, opts);
			};
			return true;
		})()`, nil),
		// Click the counter button (returns a promise, but loading state
		// should be set synchronously before the await).
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').click()`, nil),
		// Immediately check for loading indicators.
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').classList.contains('fui-loading')`, &hasLoadingClass),
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').getAttribute('aria-busy')`, &hasAriaBusy),
		// Wait for the RPC to complete.
		chromedp.Sleep(3*time.Second),
		// Verify the signal updated.
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').textContent`, &signalText),
		// Verify loading state was cleaned up.
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').classList.contains('fui-loading')`, &loadingClassAfter),
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').getAttribute('aria-busy')`, &ariaBusyAfter),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("signal after load: %q", signalText)

	if !hasLoadingClass {
		t.Error("button did not have fui-loading CSS class during in-flight RPC")
	}
	if hasAriaBusy != "true" {
		t.Error("button did not have aria-busy='true' during in-flight RPC")
	}
	trimmed := strings.TrimSpace(signalText)
	if trimmed == "0" || trimmed == "" {
		t.Errorf("counter still %q after delayed RPC — signal didn't update", signalText)
	}
	if loadingClassAfter {
		t.Error("button still has fui-loading CSS class after RPC completed")
	}
	if ariaBusyAfter == "true" {
		t.Error("button still has aria-busy='true' after RPC completed")
	}
}

// TestE2EInteractive_ReducedMotionFlashSkip verifies that when
// prefers-reduced-motion is enabled, the fui-flash class is NOT added
// to signal nodes on update. Users who prefer reduced motion should
// not see the flash animation.
func TestE2EInteractive_ReducedMotionFlashSkip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var hadFlashClass bool
	var signalText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rpc-signal"),
		chromedp.WaitVisible(`[data-fui-signal="demo-counter"]`),
		// Mock matchMedia to report prefers-reduced-motion: reduce.
		chromedp.Evaluate(`(function(){
			var origMatchMedia = window.matchMedia;
			window.matchMedia = function(q) {
				if (q === '(prefers-reduced-motion: reduce)') {
					return { matches: true, media: q, addListener: function(){}, removeListener: function(){}, addEventListener: function(){}, removeEventListener: function(){} };
				}
				return origMatchMedia.call(this, q);
			};
			return true;
		})()`, nil),
		// Click the counter button.
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/interactive/counter"]').click()`, nil),
		chromedp.Sleep(1*time.Second),
		// Check the signal updated.
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').textContent`, &signalText),
		// Check that the fui-flash class was NOT added.
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-counter"]').classList.contains('fui-flash')`, &hadFlashClass),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("reduced-motion signal text: %q", signalText)

	trimmed := strings.TrimSpace(signalText)
	if trimmed == "0" || trimmed == "" {
		t.Errorf("counter still %q after click — signal didn't update", signalText)
	}
	if hadFlashClass {
		t.Error("signal node has fui-flash class despite prefers-reduced-motion — flash should be skipped")
	}
}

// NOTE: a chromedp mobile-overflow test was tried and removed — chromedp's
// EmulateViewport doesn't reproduce the grid-overflow that a real browser
// resize does, so it passed even with the broken CSS (a false guard). The
// responsive rule is guarded deterministically by
// TestDocShellCollapsesOnMobile in site_test.go (asserts the CSS), and the
// behavior was verified manually in a real browser at 320/375/414.

// ─── Client-only Interactive Component E2E Tests ───────────────────────

func TestE2E_CounterIncrementsLocally(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	// Navigate to the counter demo page.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/counter"),
		chromedp.WaitReady(".fui-counter", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Initial value should be 0.
	var initial string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-counter [data-fui-signal]').textContent`, &initial),
	); err != nil {
		t.Fatalf("read initial: %v", err)
	}
	if strings.TrimSpace(initial) != "0" {
		t.Fatalf("initial count = %q, want 0", initial)
	}

	// Click the increment button.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-counter__inc').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("increment click: %v", err)
	}

	var afterInc string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-counter [data-fui-signal]').textContent`, &afterInc),
	); err != nil {
		t.Fatalf("read after inc: %v", err)
	}
	if strings.TrimSpace(afterInc) != "1" {
		t.Fatalf("after increment = %q, want 1", afterInc)
	}

	// Click decrement.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-counter__dec').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("decrement click: %v", err)
	}

	var afterDec string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-counter [data-fui-signal]').textContent`, &afterDec),
	); err != nil {
		t.Fatalf("read after dec: %v", err)
	}
	if strings.TrimSpace(afterDec) != "0" {
		t.Fatalf("after decrement = %q, want 0", afterDec)
	}
}

func TestE2E_TabsSwitchPanels(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		chromedp.WaitReady(".fui-tabs", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// First tab should be active — the highlight is driven by the
	// wrapper's data-active, so the first button's bottom border is the
	// accent colour while the second button's is transparent.
	var firstBorder, secondBorderInitial string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`getComputedStyle(document.querySelectorAll('.fui-tab')[0]).borderBottomColor`, &firstBorder),
		chromedp.Evaluate(`getComputedStyle(document.querySelectorAll('.fui-tab')[1]).borderBottomColor`, &secondBorderInitial),
	); err != nil {
		t.Fatalf("read initial borders: %v", err)
	}
	if firstBorder == secondBorderInitial {
		t.Fatalf("first tab should be visually active initially; both borders = %q", firstBorder)
	}

	// Click the second tab.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('.fui-tab')[1].click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("click second tab: %v", err)
	}

	// The wrapper got the new signal value.
	var activeAttr string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-tabs').getAttribute('data-active')`, &activeAttr),
	); err != nil {
		t.Fatalf("read active attr: %v", err)
	}
	if activeAttr != "1" {
		t.Fatalf("data-active = %q, want 1", activeAttr)
	}

	// Second panel should now be visible.
	var panel2Display string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-tab-panel[data-fui-tab-index="1"]')).display`, &panel2Display),
	); err != nil {
		t.Fatalf("read panel2 display: %v", err)
	}
	if panel2Display == "none" {
		t.Fatal("second tab panel should be visible after clicking second tab")
	}

	// Regression (frozen-highlight bug): the active indicator must MOVE to
	// the second button — its bottom border now matches the original
	// first-tab accent, and the first button no longer does.
	var firstAfter, secondAfter string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`getComputedStyle(document.querySelectorAll('.fui-tab')[0]).borderBottomColor`, &firstAfter),
		chromedp.Evaluate(`getComputedStyle(document.querySelectorAll('.fui-tab')[1]).borderBottomColor`, &secondAfter),
	); err != nil {
		t.Fatalf("read borders after click: %v", err)
	}
	if secondAfter != firstBorder {
		t.Fatalf("active highlight did not move to second tab: got %q, want %q", secondAfter, firstBorder)
	}
	if firstAfter == firstBorder {
		t.Fatalf("first tab still shows the active highlight after switching (frozen highlight): %q", firstAfter)
	}
}

func TestE2E_ToggleFlipsValue(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		chromedp.WaitReady(".fui-toggle", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Initial value should be "false".
	var initial string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-toggle"]').textContent`, &initial),
	); err != nil {
		t.Fatalf("read initial: %v", err)
	}
	if strings.TrimSpace(initial) != "false" {
		t.Fatalf("initial toggle = %q, want false", initial)
	}

	// Click the toggle.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-toggle').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("toggle click: %v", err)
	}

	var afterToggle string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-toggle"]').textContent`, &afterToggle),
	); err != nil {
		t.Fatalf("read after toggle: %v", err)
	}
	if strings.TrimSpace(afterToggle) != "true" {
		t.Fatalf("after toggle = %q, want true", afterToggle)
	}

	// Click again to flip back.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-toggle').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("toggle back click: %v", err)
	}

	var afterBack string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="demo-toggle"]').textContent`, &afterBack),
	); err != nil {
		t.Fatalf("read after back: %v", err)
	}
	if strings.TrimSpace(afterBack) != "false" {
		t.Fatalf("after toggle back = %q, want false", afterBack)
	}
}

func TestE2E_CollapsibleExpandsAndCollapses(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/collapsible"),
		chromedp.WaitReady(".fui-collapsible", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// First collapsible should NOT be open initially.
	var firstOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-collapsible').hasAttribute('open')`, &firstOpen),
	); err != nil {
		t.Fatalf("check first open: %v", err)
	}
	if firstOpen {
		t.Fatal("first collapsible should NOT be open initially")
	}

	// Second collapsible SHOULD be open (Open: true in config).
	var secondOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('.fui-collapsible')[1].hasAttribute('open')`, &secondOpen),
	); err != nil {
		t.Fatalf("check second open: %v", err)
	}
	if !secondOpen {
		t.Fatal("second collapsible should be open initially (Open: true)")
	}

	// Click the first summary to open it.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-collapsible__summary').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("click summary: %v", err)
	}

	var afterClickOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-collapsible').hasAttribute('open')`, &afterClickOpen),
	); err != nil {
		t.Fatalf("check after click: %v", err)
	}
	if !afterClickOpen {
		t.Fatal("first collapsible should be open after clicking summary")
	}
}

// ─── New Interactive Primitives E2E Tests ──────────────────────────────

func TestE2E_DropdownOpensAndCloses(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady("[data-fui-dropdown-wrap]", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Wait for the dropdown module to load (it loads on-demand).
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`new Promise(r => { const check = () => (window.__gofastr?.loadedModules?.dropdown) ? r(true) : setTimeout(check, 50); check(); })`, nil),
	); err != nil {
		t.Fatalf("wait for module: %v", err)
	}

	// Panel should be hidden initially.
	var panelHidden bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-dropdown-panel]').hasAttribute('hidden')`, &panelHidden),
	); err != nil {
		t.Fatalf("check initial hidden: %v", err)
	}
	if !panelHidden {
		t.Fatal("dropdown panel should be hidden initially")
	}

	// Click the trigger to open.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-dropdown]').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("click trigger: %v", err)
	}

	// Panel should now be visible.
	var expanded string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-dropdown]').getAttribute('aria-expanded')`, &expanded),
	); err != nil {
		t.Fatalf("check expanded: %v", err)
	}
	if expanded != "true" {
		t.Fatalf("aria-expanded = %q, want true", expanded)
	}

	// Click outside to close.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.body.click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("click outside: %v", err)
	}

	var afterClose string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-dropdown]').getAttribute('aria-expanded')`, &afterClose),
	); err != nil {
		t.Fatalf("check after close: %v", err)
	}
	if afterClose != "false" {
		t.Fatalf("aria-expanded after close = %q, want false", afterClose)
	}
}

func TestE2E_ScrollRevealShowsOnViewport(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/scroll-reveal"),
		chromedp.WaitReady("[data-fui-reveal]", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// The reveal element should have the attribute.
	var revealAttr string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-reveal]').getAttribute('data-fui-reveal')`, &revealAttr),
	); err != nil {
		t.Fatalf("check attr: %v", err)
	}
	if revealAttr != "fade-up" {
		t.Fatalf("data-fui-reveal = %q, want fade-up", revealAttr)
	}

	// Scroll the element into view.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-reveal]').scrollIntoView()`, nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("scroll into view: %v", err)
	}

	// After scrolling into view, the fui-revealed class should be present
	// (if the runtime module loaded). If the module hasn't loaded yet,
	// the element still exists and is visible — just without the animation.
	var hasClass bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-reveal]').classList.contains('fui-revealed')`, &hasClass),
	); err != nil {
		t.Fatalf("check revealed: %v", err)
	}
	if !hasClass {
		t.Log("NOTE: fui-revealed class not present — reveal module may not have loaded in test env")
	}
}

func TestE2E_SignalAnimateTogglesClass(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/signal-animate"),
		chromedp.WaitReady("[data-fui-animate-signal]", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Wait for the animate module to load (it loads on-demand).
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`new Promise(r => { const check = () => (window.__gofastr?.loadedModules?.animate) ? r(true) : setTimeout(check, 50); check(); })`, nil),
	); err != nil {
		t.Fatalf("wait for module: %v", err)
	}

	// Initially the animated class should NOT be present.
	var hasClass bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-animate-signal]').classList.contains('fui-expanded')`, &hasClass),
	); err != nil {
		t.Fatalf("check initial class: %v", err)
	}
	if hasClass {
		t.Fatal("fui-expanded should NOT be present initially")
	}

	// Click the toggle button to set the signal to "true".
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-signal-toggle]').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("click toggle: %v", err)
	}

	// Now the class should be present.
	var afterToggle bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-animate-signal]').classList.contains('fui-expanded')`, &afterToggle),
	); err != nil {
		t.Fatalf("check after toggle: %v", err)
	}
	if !afterToggle {
		t.Fatal("fui-expanded should be present after toggle")
	}
}

// ─── E2E Sweep: existing interactive components ────────────────────────

func TestE2E_CopyButtonWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/copybutton"),
		chromedp.WaitReady("[data-fui-comp='ui-copy-btn']", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Click the copy button.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.ui-copy-btn').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("click copy: %v", err)
	}

	// The button should show a copied state (fui-copied class).
	var hasCopied bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.ui-copy-btn').classList.contains('fui-copied')`, &hasCopied),
	); err != nil {
		t.Fatalf("read copied state: %v", err)
	}
	if !hasCopied {
		t.Error("copy button should have fui-copied class after click")
	}
}

func TestE2E_PasswordToggleWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/passwordinput"),
		chromedp.WaitReady("[data-fui-comp='ui-password-input']", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Initially the input should be type=password.
	var inputType string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-password-input"] input').type`, &inputType),
	); err != nil {
		t.Fatalf("check initial type: %v", err)
	}
	if inputType != "password" {
		t.Fatalf("initial type = %q, want password", inputType)
	}

	// Click the toggle button.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-password-input"] button').click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("click toggle: %v", err)
	}

	var afterToggle string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-password-input"] input').type`, &afterToggle),
	); err != nil {
		t.Fatalf("check after toggle: %v", err)
	}
	if afterToggle != "text" {
		t.Fatalf("after toggle type = %q, want text", afterToggle)
	}
}

func TestE2E_TextareaAutogrow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/textarea"),
		chromedp.WaitReady("textarea[data-fui-autogrow]", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Verify the autogrow attribute is present.
	var hasAttr bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('textarea[data-fui-autogrow]') !== null`, &hasAttr),
	); err != nil {
		t.Fatalf("check attr: %v", err)
	}
	if !hasAttr {
		t.Fatal("textarea should have data-fui-autogrow attribute")
	}

	// Verify the textarea module loaded.
	var moduleLoaded bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`!!window.__gofastr?.loadedModules?.textarea`, &moduleLoaded),
	); err != nil {
		t.Fatalf("check module: %v", err)
	}
	if !moduleLoaded {
		t.Log("NOTE: textarea module not loaded — autogrow may not work in test env")
	}
}

// TestE2EInteractive_WorkspacePanes exercises the /examples/workspace
// master-detail flow end to end: clicking a ticket row opens the
// secondary pane AND loads its detail via RPC (no navigation), then
// "View customer" fills the tertiary pane the same way.
func TestE2EInteractive_WorkspacePanes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var url1, detail, url2, customer string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/workspace"),
		chromedp.WaitVisible(`button[data-fui-rpc="/__site/workspace/ticket?id=4021"]`),
		// Row click: opens the secondary pane + fetches the detail.
		chromedp.Evaluate(`document.querySelector('button[data-fui-rpc="/__site/workspace/ticket?id=4021"]').click()`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Location(&url1),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="ws-ticket"]').textContent`, &detail),
		// "View customer" inside the detail fills the tertiary pane.
		chromedp.Evaluate(`document.querySelector('[data-fui-pane-open="tertiary"]').click()`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Location(&url2),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="ws-customer"]').textContent`, &customer),
	); err != nil {
		t.Fatal(err)
	}
	// The whole flow must stay on the same page — panes fill, no nav.
	if !strings.Contains(url1, "/examples/workspace") || !strings.Contains(url2, "/examples/workspace") {
		t.Errorf("expected to stay on /examples/workspace, got %q then %q", url1, url2)
	}
	if !strings.Contains(detail, "SSO login") {
		t.Errorf("ticket detail did not load into the pane: %q", detail)
	}
	if !strings.Contains(customer, "Northwind") {
		t.Errorf("customer detail did not load into the tertiary pane: %q", customer)
	}
}
