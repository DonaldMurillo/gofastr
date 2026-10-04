package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// labServer serves one app's router for a browser test.
func labServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// labVTArm makes every view transition long-observable (30s) by
// adopting a stylesheet that overrides the transition group's duration.
// Constructable stylesheets are CSSOM, so the strict CSP does not block
// them (an injected <style> element would be). Also records every
// gofastr:transition event into window.__labVT.
func labVTArm(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const sheet = new CSSStyleSheet();
		sheet.replaceSync('::view-transition-group(*), ::view-transition-image-pair(*) { animation-duration: 30s; }');
		document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
		window.__labVT = [];
		document.addEventListener('gofastr:transition', (e) => {
			window.__labVT.push({ from: e.detail.from, to: e.detail.to, types: [...e.detail.types] });
		});
		return true;
	})()`, nil)); err != nil {
		t.Fatalf("arm: %v", err)
	}
}

// labVTActive reports document.activeViewTransition's types, or "" when
// no transition is active.
// labVTActive reports document.activeViewTransition's types, or "" when
// no transition is active. ViewTransition.types is a setlike
// (ViewTransitionTypeSet in Chrome), so spread it, don't array-method it.
func labVTActive(t *testing.T, ctx context.Context) string {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const vt = document.activeViewTransition; return vt ? [...(vt.types || [])].join(',') : ''; })()`, &out)); err != nil {
		t.Fatalf("active: %v", err)
	}
	return out
}

// labVTEvents returns the recorded gofastr:transition events.
func labVTEvents(t *testing.T, ctx context.Context) []map[string]any {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__labVT || [])`, &raw)); err != nil {
		t.Fatalf("events: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("events json: %v", err)
	}
	return out
}

// navigation's swap rides document.startViewTransition with the
// direction as a type (forward on click, back on popstate going back,
// reload on refresh()), a second navigation skips the running one, and
// preventDefault on gofastr:transition commits the swap with NO
// transition.
func TestViewTransitionWrapperE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx, chromedp.Navigate(base+"/settings")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	labVTArm(t, tctx)

	wait := chromedp.Sleep(200 * time.Millisecond)

	// Forward: click nav to /inbox.
	labClickNav(t, tctx, "/inbox")
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery), wait); err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if got := labVTActive(t, tctx); got != "forward" {
		t.Errorf("after click nav: activeViewTransition.types = %q, want %q", got, "forward")
	}
	evs := labVTEvents(t, tctx)
	if len(evs) != 1 || evs[0]["from"] != "/settings" || evs[0]["to"] != "/inbox" {
		t.Errorf("gofastr:transition events = %v, want one /settings -> /inbox", evs)
	} else if types, _ := evs[0]["types"].([]any); len(types) != 1 || types[0] != "forward" {
		t.Errorf("event types = %v, want [forward]", evs[0]["types"])
	}

	// Back: popstate to /settings carries the 'back' type (the running
	// forward transition is skipped, not awaited).
	labBack(t, tctx)
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery), wait); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if got := labVTActive(t, tctx); got != "back" {
		t.Errorf("after Back: activeViewTransition.types = %q, want %q", got, "back")
	}
	evs = labVTEvents(t, tctx)
	if len(evs) != 2 || evs[1]["to"] != "/settings" {
		t.Errorf("gofastr:transition events = %v, want a second one to /settings", evs)
	}

	// reload: refresh() re-renders the current screen.
	if err := chromedp.Run(tctx, page.BringToFront(), chromedp.Evaluate(`window.__gofastr.refresh()`, nil), wait); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery)); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	if got := labVTActive(t, tctx); got != "reload" {
		t.Errorf("after refresh(): activeViewTransition.types = %q, want %q", got, "reload")
	}

	// Cancelled: preventDefault on gofastr:transition commits the swap
	// synchronously with NO transition. The click is programmatic: the
	// 30s-armed transition from refresh() is still active, and while a
	// view transition animates the snapshot overlay owns hit-testing
	// (elementFromPoint at a link returns <html>), so a coordinate
	// click would be eaten by the platform. That platform behavior is
	// itself part of the P11 measurement (see the report).
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(`document.addEventListener('gofastr:transition', (e) => e.preventDefault(), { once: true }); true`, nil),
	); err != nil {
		t.Fatalf("arm cancel: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.Evaluate(`document.querySelector('a[data-nav="/inbox"]').click(); true`, nil)); err != nil {
		t.Fatalf("click inbox: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery), wait); err != nil {
		t.Fatalf("inbox (cancelled): %v", err)
	}
	// No NEW transition: the refresh() reload transition is still
	// active (30s-armed), so "no active transition" cannot be asserted
	// directly — assert the active one is still that SAME object and
	// not a transition this navigation started.
	var same string
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`(() => { const vt = document.activeViewTransition; return vt && vt.types && vt.types.has ? (vt.types.has('reload') ? 'reload-still' : 'new:' + [...vt.types].join(',')) : 'none'; })()`, &same)); err != nil {
		t.Fatalf("same check: %v", err)
	}
	if same != "reload-still" {
		t.Errorf("a cancelled transition must not start one; activeViewTransition reads %q", same)
	}
	evs = labVTEvents(t, tctx)
	if len(evs) != 4 {
		t.Errorf("gofastr:transition fired %d times, want 4 (forward, back, reload, cancelled)", len(evs))
	}
}

// TestViewTransitionMirrorE2E pins the CSP-safe name assignment: after
// a navigation into the /items group the detail primary's cell (marked
// data-cui-vt="vt-items-primary" by the server, P11-B generated name)
// carries the CSSOM view-transition-name, the marker itself is in the
// SSR bytes, and the generated rules ship in app.css.
func TestViewTransitionMirrorE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL

	// JS-off first: the marker is server-rendered, and the generated
	// CSS (name assignment + back variant) rides app.css.
	res, err := http.Get(base + "/items/1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), `data-cui-vt="vt-items-primary"`) {
		t.Errorf("SSR /items/1 missing data-cui-vt=\"vt-items-primary\"")
	}
	cssRes, err := http.Get(base + "/__gofastr/app.css")
	if err != nil {
		t.Fatalf("get app.css: %v", err)
	}
	css, _ := io.ReadAll(cssRes.Body)
	cssRes.Body.Close()
	for _, want := range []string{
		`[data-cui-vt="vt-items-primary"]`,
		`:root:active-view-transition-type(back) ::view-transition-new(vt-items-primary)`,
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("app.css missing generated rule %q", want)
		}
	}

	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)
	if err := chromedp.Run(tctx, chromedp.Navigate(base+"/settings")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	labVTArm(t, tctx)
	labClickNav(t, tctx, "/items/1")
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("detail: %v", err)
	}
	var name string
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`(() => { const el = document.querySelector('[data-cui-vt="vt-items-primary"]'); return el ? el.getAttribute('data-cui-vt') + '=' + el.style.viewTransitionName : '!missing'; })()`, &name)); err != nil {
		t.Fatalf("mirror read: %v", err)
	}
	if name != "vt-items-primary=vt-items-primary" {
		t.Errorf("CSSOM viewTransitionName = %q, want the marker mirrored (a style attribute would break the CSP)", name)
	}
}

// TestViewTransitionReducedMotionE2E pins the default-not-animate rule:
// under prefers-reduced-motion: reduce no transition starts (and the
// gofastr:transition event does not fire), navigation still works.
func TestViewTransitionReducedMotionE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("force-prefers-reduced-motion", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	root, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	if err := chromedp.Run(root); err != nil {
		t.Fatalf("browser failed to start: %v", err)
	}

	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	ctx, cancel2 := chromedp.NewContext(root)
	t.Cleanup(cancel2)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx, chromedp.Navigate(base+"/settings")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	labVTArm(t, tctx)
	labClickNav(t, tctx, "/inbox")
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if got := labVTActive(t, tctx); got != "" {
		t.Errorf("reduced motion: a transition started anyway (types=%q); the default must not animate", got)
	}
	if evs := labVTEvents(t, tctx); len(evs) != 0 {
		t.Errorf("reduced motion: gofastr:transition fired %v times; it must not fire when no transition can run", len(evs))
	}
}
