package main

// Error pages are real pages (2026-09-27 brief): they finish through
// the same page tail every document runs (session mint, no-store, seed,
// widget SSR), so leaving them swaps inside the live shell instead of
// dying on a 409 part reset, entering them works from every chain, and
// the widget catalog answers on a direct error load. Real mouse input
// throughout (mouseClickAt/nodeCenter, never el.click()).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// armNetRecorder stamps a window marker (a full document load clears
// window state — surviving it proves no reload happened) and wraps
// fetch so every response's status and part-reset header are recorded:
// the "no 409 on the wire" assertion reads the wrapper, not the server.
func armNetRecorder(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__marker = 42;
		window.__net = [];
		const of = window.fetch;
		window.fetch = function(u, o) {
			return of.apply(this, arguments).then((r) => {
				try {
					window.__net.push(String(u) + ' ' + r.status + ' ' + (r.headers.get('X-Gofastr-Part-Reset') || ''));
				} catch (_) {}
				return r;
			});
		};
		return true;
	})()`, nil)); err != nil {
		t.Fatalf("arm recorder: %v", err)
	}
}

// net409s returns the recorded 409 responses.
func net409s(t *testing.T, ctx context.Context) []string {
	t.Helper()
	var net []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__net || []`, &net)); err != nil {
		t.Fatalf("read recorder: %v", err)
	}
	var out []string
	for _, e := range net {
		if strings.Contains(e, " 409 ") {
			out = append(out, e)
		}
	}
	return out
}

// markerSurvived reports whether the window marker is still in place
// (any document navigation wipes it).
func markerSurvived(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var m int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__marker || 0`, &m)); err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return m == 42
}

// clickSel real-mouse-clicks the first element matching sel at its
// centre (the brief's contract: never el.click()).
func clickSel(t *testing.T, ctx context.Context, sel string) {
	t.Helper()
	x, y := nodeCenter(t, ctx, sel)
	mouseClickAt(t, ctx, x, y)
}

// assertNo409AndNoReload is the shared post-click assertion pair.
func assertNo409AndNoReload(t *testing.T, ctx context.Context, phase string) {
	t.Helper()
	if got := net409s(t, ctx); len(got) > 0 {
		t.Errorf("%s: 409 responses on the wire (a part reset reloads the document): %v", phase, got)
	}
	if !markerSurvived(t, ctx) {
		t.Errorf("%s: the window marker is gone — the document navigated (reload), not the region", phase)
	}
}

// TestWidgetsLoadOnErrorPage: a DIRECT 404 load mints a session into
// the page chrome (finishPageDocument), so the session-gated widget
// catalog answers 200 — before the fix the chrome carried no session id
// and /__gofastr/widgets answered 401.
func TestWidgetsLoadOnErrorPage(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 60*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/unknown-project"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		waitPageTextReady("Page not found"),
	); err != nil {
		t.Fatalf("navigate 404: %v", err)
	}

	// The chrome carries a live session id (the SSE meta).
	var sse string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => (document.querySelector('meta[name="gofastr-sse"]') || {}).content || '')()`, &sse)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sse, "session=") {
		t.Errorf("404 chrome carries no session id (gofastr-sse meta = %q)", sse)
	}

	// The catalog endpoint answers 200 with the page's own session.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`fetch('/__gofastr/widgets?page=/projects/unknown-project').then((r) => { window.__wstat = r.status; return r.status; })`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Poll(`window.__wstat !== undefined && window.__wstat !== null`, nil,
			chromedp.WithPollingTimeout(15*time.Second)),
	); err != nil {
		t.Fatalf("widget catalog fetch never settled: %v", err)
	}
	var status int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__wstat`, &status)); err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Errorf("/__gofastr/widgets on a direct 404 load = %d, want 200 (the error page must mint like a page)", status)
	}
}

func waitPageTextReady(want string) chromedp.Action {
	return chromedp.Poll(fmt.Sprintf(
		`(() => { const m = document.querySelector('main'); return m && m.textContent.includes(%q); })()`, want), nil)
}

// TestLeaveErrorPageSwapsWithoutReload: from a direct 404 load and a
// direct 500 load, real-mouse navigation out of the error page swaps
// regions inside the live shell — no 409 on the wire, no document
// navigation, the right regions swapped, the deferred part applied.
func TestLeaveErrorPageSwapsWithoutReload(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	// --- the 404 document -------------------------------------------------
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/unknown-project"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		waitPageTextReady("Page not found"),
	); err != nil {
		t.Fatalf("navigate 404: %v", err)
	}
	armNetRecorder(t, ctx)

	// "Back to overview": swaps at the shell slot, the overview renders.
	clickSel(t, ctx, `main a.fui-button`)
	waitPageText(t, ctx, `main`, "open")
	assertNo409AndNoReload(t, ctx, "404 → Back to overview")
	if got := readText(t, ctx, crumbsSel); !strings.Contains(got, "Home") {
		t.Errorf("crumbs after overview = %q, want Home", got)
	}

	// A sidebar project link out of a fresh 404.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/unknown-project"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		waitPageTextReady("Page not found"),
	); err != nil {
		t.Fatalf("re-navigate 404: %v", err)
	}
	armNetRecorder(t, ctx)
	clickSel(t, ctx, `.fui-content-row__nav a[href="/projects/search"]`)
	waitText(t, ctx, `main h1`, "Search")
	assertNo409AndNoReload(t, ctx, "404 → sidebar project link")
	if got := readText(t, ctx, crumbsSel); !strings.Contains(got, "Search") {
		t.Errorf("crumbs after project = %q, want Search", got)
	}

	// An issue link from there: the project layer is live, the click
	// swaps the innermost slot and the deferred aside part lands.
	clickSel(t, ctx, `a.fui-card[data-key][data-key="SRCH-8"]`)
	waitText(t, ctx, `#issue-detail h2`, "Typo")
	assertNo409AndNoReload(t, ctx, "project → issue link")
	waitText(t, ctx, asideSel, "reported this issue") // the real feed (not the skeleton, whose label also says Activity)

	// --- the 500 document -------------------------------------------------
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/legacy/issues/40"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		waitPageTextReady("Something went wrong"),
	); err != nil {
		t.Fatalf("navigate 500: %v", err)
	}
	armNetRecorder(t, ctx)

	// "Back to overview" out of the 500.
	clickSel(t, ctx, `main a.fui-button`)
	waitPageText(t, ctx, `main`, "open")
	assertNo409AndNoReload(t, ctx, "500 → Back to overview")

	// The same-project case: the error URL's resolved chain matches the
	// Legacy project's, so the server's From-depth points below a layer
	// the DOM does not hold; the runtime must still land a client swap
	// (its deploy-skew repair fetches the document and swaps the shell
	// region) — never a document navigation.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/legacy/issues/40"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		waitPageTextReady("Something went wrong"),
	); err != nil {
		t.Fatalf("re-navigate 500: %v", err)
	}
	armNetRecorder(t, ctx)
	clickSel(t, ctx, `.fui-content-row__nav a[href="/projects/legacy"]`)
	waitText(t, ctx, `main h1`, "Legacy")
	assertNo409AndNoReload(t, ctx, "500 → same-project sidebar link")

	// An issue link out of the 500 into another project's chain.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/legacy/issues/40"),
		chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
		waitPageTextReady("Something went wrong"),
	); err != nil {
		t.Fatalf("re-navigate 500 (issue): %v", err)
	}
	armNetRecorder(t, ctx)
	clickSel(t, ctx, `.fui-content-row__nav a[href="/projects/billing"]`)
	waitText(t, ctx, `main h1`, "Billing")
	clickSel(t, ctx, `a.fui-card[data-key][data-key="BIL-42"]`)
	waitText(t, ctx, `#issue-detail h2`, "Retry")
	assertNo409AndNoReload(t, ctx, "500 → project → issue link")
	waitText(t, ctx, asideSel, "reported this issue")
}

// TestEnterErrorPageFromEveryChain: from /, /reports, a project index
// and an issue page, entering the 404 and the 500 shows the error page
// INSIDE the live shell, and Back restores the origin with no reload.
// No in-app link targets a missing route or the broken store, so the
// entry goes through the runtime's own navigate() — the same loadPage a
// click reaches (the leave test covers the click path with real mouse).
func TestEnterErrorPageFromEveryChain(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 180*time.Second)

	origins := []struct {
		path string
		mark string // breadcrumb/heading text proving the origin rendered
	}{
		{"/", "Home"},
		{"/reports", "Reports"},
		{"/projects/billing", "Billing"},
		{"/projects/billing/issues/42", "BIL-42"},
	}
	errs := []struct {
		path string
		want string
		name string
	}{
		{"/projects/unknown-project", "Page not found", "404"},
		{"/projects/legacy/issues/40", "Something went wrong", "500"},
	}
	for _, o := range origins {
		for _, e := range errs {
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+o.path),
				chromedp.WaitVisible(`.fui-content-row__nav`, chromedp.ByQuery),
				chromedp.Poll(crumbContains(o.mark), nil, chromedp.WithPollingTimeout(20*time.Second)),
			); err != nil {
				t.Fatalf("origin %s: %v", o.path, err)
			}
			armNetRecorder(t, ctx)
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`window.__gofastr.navigate('`+e.path+`')`, nil)); err != nil {
				t.Fatalf("navigate %s: %v", e.path, err)
			}
			waitPageText(t, ctx, `main h1`, e.want)
			if got := readText(t, ctx, `.fui-content-row__nav`); !strings.Contains(got, "Projects") {
				t.Errorf("%s from %s: the shell must survive (sidebar = %q)", e.name, o.path, got)
			}
			// The error page's own deferred-aside part may answer 409
			// (its policy phase sees the same dead store/route as the
			// page) — that is on the wire BY DESIGN; what must never
			// happen is the reload: an error-page outcome discards its
			// parts (the page-outcome rule), so only the marker check
			// applies here.
			if !markerSurvived(t, ctx) {
				t.Errorf("%s from %s: the window marker is gone — the part's 409 reset reloaded the document instead of dying with the error page", e.name, o.path)
			}
			var url string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &url)); err != nil {
				t.Fatal(err)
			}
			if url != e.path {
				t.Errorf("%s from %s: URL = %s, want %s", e.name, o.path, url, e.path)
			}
			// Back restores the origin, still without a reload.
			if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
				t.Fatal(err)
			}
			if err := chromedp.Run(ctx, chromedp.Poll(crumbContains(o.mark), nil,
				chromedp.WithPollingTimeout(20*time.Second))); err != nil {
				t.Fatalf("%s from %s: origin did not come back: %v", e.name, o.path, err)
			}
			if !markerSurvived(t, ctx) {
				t.Errorf("Back from %s to %s: the window marker is gone — the document reloaded", e.name, o.path)
			}
		}
	}
}

func crumbContains(s string) string {
	return fmt.Sprintf(
		`(() => { const c = document.querySelector('[data-fui-area="l:shell~crumbs"]'); return c && c.textContent.includes(%q); })()`, s)
}

// TestTrackerCurrentMarksFollowNavigation: after client navigations the
// sidebar entry and the issue row marked current match the URL — the
// marks are the runtime's (activelink), and a stale first-paint mark
// must never survive beside the fresh one.
func TestTrackerCurrentMarksFollowNavigation(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 90*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/42"),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate BIL-42: %v", err)
	}
	// Let the idle-loaded activelink module run its first pass.
	time.Sleep(700 * time.Millisecond)

	current := func(sel string) []string {
		var out []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('`+sel+` [aria-current="page"], `+sel+`[aria-current="page"]')).map((a) => a.getAttribute('href') || a.getAttribute('data-key'))`, &out)); err != nil {
			t.Fatal(err)
		}
		return out
	}
	currentRow := func() []string {
		var out []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('.fui-list-detail__list a.fui-card[data-key][aria-current="page"]')).map((a) => a.getAttribute('data-key'))`, &out)); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Direct load: the sidebar's Billing entry and the BIL-42 row.
	if got := current(`.fui-sidebar__inline`); len(got) != 1 || got[0] != "/projects/billing" {
		t.Errorf("sidebar current on direct issue load = %v, want exactly [/projects/billing]", got)
	}
	if got := currentRow(); len(got) != 1 || got[0] != "BIL-42" {
		t.Errorf("current row on direct issue load = %v, want exactly [BIL-42]", got)
	}

	// Click BIL-31 with a real mouse: the row mark moves, the old row's
	// is gone (the stale-mark bug lit two rows).
	clickSel(t, ctx, `a.fui-card[data-key][data-key="BIL-31"]`)
	if err := chromedp.Run(ctx, chromedp.Poll(
		`(() => { const a = document.querySelector('.fui-list-detail__list a[aria-current="page"]'); return a && a.getAttribute('data-key') === 'BIL-31'; })()`,
		nil, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
		t.Fatalf("row mark never moved to BIL-31: %v", err)
	}
	if got := currentRow(); len(got) != 1 || got[0] != "BIL-31" {
		t.Errorf("current row after click = %v, want exactly [BIL-31] (a stale mark survived)", got)
	}
	if got := current(`.fui-sidebar__inline`); len(got) != 1 || got[0] != "/projects/billing" {
		t.Errorf("sidebar current after issue click = %v, want [/projects/billing]", got)
	}

	// Sidebar to another project: the entry mark moves too.
	clickSel(t, ctx, `.fui-content-row__nav a[href="/projects/search"]`)
	waitText(t, ctx, `main h1`, "Search")
	if err := chromedp.Run(ctx, chromedp.Poll(
		`(() => { const a = document.querySelector('.fui-content-row__nav a[aria-current="page"]'); return a && a.getAttribute('href') === '/projects/search'; })()`,
		nil, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
		t.Fatalf("sidebar mark never moved to Search: %v", err)
	}
	if got := current(`.fui-sidebar__inline`); len(got) != 1 || got[0] != "/projects/search" {
		t.Errorf("sidebar current after project click = %v, want exactly [/projects/search] (Billing's mark survived)", got)
	}
}

// TestPartSessionResetRetriesOnce (the real-app half; the runtime rig
// in core-ui/runtime pins the once-only and reload-once numbers): a
// session that dies between page loads — here the server stops seeing
// the cookie, exactly what a rotated key or an expired token looks
// like to it — must cost ONE part retry after the page commit, never a
// document reload. The page request re-mints (Set-Cookie), the part
// answers 409 session, the retry lands the aside.
func TestPartSessionResetRetriesOnce(t *testing.T) {
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	var expire int32
	// A rotation, not a blackout: the first cookie value the server
	// ever sees is the OLD token — strip exactly that one, so the
	// re-minted token the page answer Set-Cookies lands and is
	// presented. Stripping every cookie forever would reset the RETRY
	// too (the once-reload case the rig pins).
	var mu sync.Mutex
	var oldToken string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A slow page answer pins the ordering the retry depends on:
		// the part's 409 must land while the page is still in flight
		// (the wait-for-commit path), never after the new cookie is
		// already stored.
		if atomic.LoadInt32(&expire) == 1 && r.Header.Get("X-Gofastr-Navigate") == "1" &&
			r.Header.Get("X-Gofastr-Part") == "" && r.URL.Path == "/projects/billing/issues/42" {
			time.Sleep(400 * time.Millisecond)
		}
		if c := r.Header.Get("Cookie"); c != "" {
			mu.Lock()
			if oldToken == "" {
				oldToken = c
			}
			dead := atomic.LoadInt32(&expire) == 1 && c == oldToken
			mu.Unlock()
			if dead {
				r.Header.Del("Cookie")
			}
		}
		app.Router().ServeHTTP(w, r)
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 90*time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate billing: %v", err)
	}
	armNetRecorder(t, ctx)
	atomic.StoreInt32(&expire, 1)

	// Click an issue row with a real mouse: the page request mints a
	// fresh session; the deferred-aside part beside it carries the now
	// invisible one and answers 409 session.
	clickSel(t, ctx, `a.fui-card[data-key][data-key="BIL-42"]`)
	waitText(t, ctx, `#issue-detail h2`, "Retry")
	waitText(t, ctx, asideSel, "reported this issue") // the retried part applied
	time.Sleep(400 * time.Millisecond)                // wait out any would-be reload

	if !markerSurvived(t, ctx) {
		t.Fatal("the window marker is gone — the session reset reloaded the document instead of retrying the part")
	}
	var m map[string]int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const net = window.__net || [];
		let resets = 0, aside = 0;
		for (const e of net) {
			if (e.indexOf(' 409 session') >= 0) resets++;
			if (e.indexOf('/projects/billing/issues/42') === 0 && e.indexOf(' 200 ') >= 0) aside++;
		}
		return { resets, aside };
	})()`, &m)); err != nil {
		t.Fatal(err)
	}
	if m["resets"] == 0 {
		t.Error("no session reset observed on the wire — the rig never exercised the retry")
	}
	if m["aside"] != 2 {
		t.Errorf("aside part requests answered 200 = %d, want exactly 2 (the reset + the one retry; more is a loop)", m["aside"])
	}
}
