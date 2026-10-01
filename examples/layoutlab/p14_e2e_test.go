package main

// PROTOTYPE (spike/layout-static): P14 browser e2e — error pages that
// SHOW. A navigation to a missing route must land on the not-found page
// INSIDE the shell (the server renders it through the root layout; the
// runtime applies a non-OK HTML answer through the same swap paths a
// 200 takes), a static host's miss serves the exported 404.html the
// same way, a non-HTML 404 still toasts but NAMES the status, and the
// lab's fallbacks (the aside's Default, an error boundary's fallback)
// are visibly labelled.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// toastText reads the nav toast's text and whether it is showing.
func toastText(t *testing.T, ctx context.Context) (string, bool) {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const el = document.querySelector('.fui-nav-toast');
		if (!el) return '';
		return (el.classList.contains('is-visible') ? '1' : '0') + el.textContent;
	})()`, &out)); err != nil {
		t.Fatalf("read toast: %v", err)
	}
	if out == "" {
		return "", false
	}
	return out[1:], out[0] == '1'
}

// assertNoToast fails when the nav toast is showing.
func assertNoToast(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	if txt, shown := toastText(t, ctx); shown {
		t.Errorf("toast showing after %s: %q", where, txt)
	}
}

// labBadgeAttr reads the aside's fallback badge attribute and its
// rendered ::before content (the badge text lives in CSS pseudo content
// so the region's textContent stays exact — the labelled-region trick).
func labBadgeAttr(t *testing.T, ctx context.Context) (attr, css string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const el = document.querySelector('[data-fui-outlet="l:shell#aside"] [data-lab-badge]');
		if (!el) return '|';
		const cs = getComputedStyle(el, '::before');
		return el.getAttribute('data-lab-badge') + '|' + (cs ? cs.content : '');
	})()`, &attr)); err != nil {
		t.Fatalf("read badge: %v", err)
	}
	if i := strings.LastIndexByte(attr, '|'); i >= 0 {
		return attr[:i], attr[i+1:]
	}
	return "", ""
}

// assertNotFoundInShell asserts the not-found page's shape inside the
// shell: header present, <main> carrying the error body, outlets at
// their defaults, crumbs naming the requested path. Evaluate-based
// reads, not WaitVisible: on the click path the 404 arrives as a fresh
// cross-document load and chromedp's visibility polling does not cross
// that boundary (measured: marker present, readyState complete, poll
// still times out).
func assertNotFoundInShell(t *testing.T, ctx context.Context) {
	t.Helper()
	if got := labRead(t, ctx, `#shell-header nav`); !strings.Contains(got, "Missing page") {
		t.Errorf("shell header gone or missing the link; nav reads %q", got)
	}
	if got := labRead(t, ctx, `main[data-fui-layout-slot="l:shell"]`); !strings.Contains(got, "404: Page not found") {
		t.Errorf("main slot = %q, want the not-found body", got)
	}
	if got := labRead(t, ctx, asideSel); got != "ASIDE-HELP" {
		t.Errorf("aside = %q, want the default ASIDE-HELP", got)
	}
	if got := labRead(t, ctx, toolbarSel); got != "" {
		t.Errorf("toolbar = %q, want empty", got)
	}
	if got := labRead(t, ctx, crumbsSel); got != "crumbs:/nope" {
		t.Errorf("crumbs = %q, want crumbs:/nope", got)
	}
}

// TestP14LiveNotFoundShowsInShell: navigating to a missing route on a
// live server must show the not-found page rendered THROUGH the shell —
// header still present, <main> carrying the error body, outlets at
// their defaults — at the /nope URL (so Back works), with no toast.
// The SPA path (navigate(), the non-OK envelope apply) and the click
// path (an unknown route falls through to a full document load, which
// now also renders through the shell) are both covered.
func TestP14LiveNotFoundShowsInShell(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	// --- SPA path: navigate() fetches a non-OK partial, the runtime
	// applies it inside the live shell. ---
	var url string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items/2"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/nope')`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Location(&url),
	); err != nil {
		t.Fatalf("navigate to /nope: %v", err)
	}
	if !strings.HasSuffix(url, "/nope") {
		t.Errorf("URL after navigate to missing page = %q, want it to STAY on /nope", url)
	}
	assertNotFoundInShell(t, tctx)
	assertNoToast(t, tctx, "the SPA 404 navigation")
	// Back returns to the item page with its content (same document,
	// the entry replays from the screen cache).
	labBack(t, tctx)
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("back to items/2: %v", err)
	}
	if got := labRead(t, tctx, `main[data-fui-layout-slot="l:shell"]`); !strings.Contains(got, "SCREEN-DETAIL-2") {
		t.Errorf("main slot after Back = %q, want the item page back", got)
	}

	// --- Click path: an anchor whose route the manifest does not know
	// falls through to a full document load; the 404 document itself
	// now renders through the shell. ---
	labClickNav(t, tctx, "/nope")
	if err := chromedp.Run(tctx,
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Location(&url),
	); err != nil {
		t.Fatalf("settle after click: %v", err)
	}
	if !strings.HasSuffix(url, "/nope") {
		t.Errorf("URL after missing-page click = %q, want /nope", url)
	}
	assertNotFoundInShell(t, tctx)
	assertNoToast(t, tctx, "the full-load 404 navigation")
}

// TestP14StaticServes404Page: the export writes 404.html and a static
// host serves it (status 404) on a miss. Clicking a link the export
// left out (/broken/load — excluded on purpose, its fill fails) shows
// the not-found page inside the shell, no toast, URL kept.
func TestP14StaticServes404Page(t *testing.T) {
	srv := labExportStatic(t)

	// The miss answers 404.html with status 404, HTML content type.
	res, err := http.Get(srv.URL + "/broken/load")
	if err != nil {
		t.Fatalf("get miss: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("miss status = %d, want 404", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("miss Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(string(body), "404: Page not found") {
		t.Errorf("404.html body lacks the not-found page; got:\n%.400s", body)
	}
	if !strings.Contains(string(body), `data-fui-layout="shell"`) {
		t.Errorf("404.html lacks the root layout shell (the runtime needs its chain to swap at layer 0)")
	}

	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	// /broken/load IS in the route manifest (the graph lists every
	// registered route), so the click is an SPA fetch; the host answers
	// 404.html with status 404 and the runtime applies the non-OK
	// document through the doc-envelope reader, inside the shell.
	var url string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items/1"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate items/1: %v", err)
	}
	labClickNav(t, tctx, "/broken/load")
	if err := chromedp.Run(tctx,
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Location(&url),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if !strings.HasSuffix(url, "/broken/load") {
		t.Errorf("URL = %q, want it to stay on /broken/load", url)
	}
	if got := labRead(t, tctx, `#shell-header nav`); !strings.Contains(got, "Missing page") {
		t.Errorf("shell header gone; nav reads %q", got)
	}
	if got := labRead(t, tctx, `main[data-fui-layout-slot="l:shell"]`); !strings.Contains(got, "404: Page not found") {
		t.Errorf("main slot = %q, want the not-found body applied from the 404.html document", got)
	} else if strings.Contains(got, "No route matched") {
		// 404.html is rendered once at build time; any path it printed
		// would be the build-time one, not the URL the visitor asked for.
		t.Errorf("main slot = %q, want no build-time path in the static 404 body", got)
	}
	if got := labRead(t, tctx, asideSel); got != "ASIDE-HELP" {
		t.Errorf("aside = %q, want the 404 page's default ASIDE-HELP", got)
	}
	assertNoToast(t, tctx, "the static miss navigation")
}

// TestP14NonHTML404ToastsStatus: a 404 whose body is NOT HTML cannot be
// applied; the toast must name the status ("(HTTP 404)"), not claim a
// connection problem, and the URL reverts.
func TestP14NonHTML404ToastsStatus(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/settings"),
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/nope-plain')`, nil),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate to plain 404: %v", err)
	}
	txt, shown := toastText(t, tctx)
	if !shown {
		t.Fatal("no toast shown for the non-HTML 404 navigation")
	}
	if !strings.Contains(txt, "(HTTP 404)") {
		t.Errorf("toast = %q, want it to name (HTTP 404)", txt)
	}
	if strings.Contains(txt, "check your connection") {
		t.Errorf("toast = %q, must not claim a connection problem for an HTTP status error", txt)
	}
	var url string
	if err := chromedp.Run(tctx, chromedp.Location(&url)); err != nil {
		t.Fatalf("location: %v", err)
	}
	if !strings.HasSuffix(url, "/settings") {
		t.Errorf("URL = %q, want reverted to /settings", url)
	}
}

// TestP14FallbacksLabelled: the aside degraded to its Default and a
// fill's own error fallback each carry a badge naming which fallback is
// showing (attribute text, drawn by CSS).
func TestP14FallbacksLabelled(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/broken/load"),
		chromedp.WaitVisible(`#lab-SCREEN-BROKEN-LOAD`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate broken/load: %v", err)
	}
	attr, css := labBadgeAttr(t, tctx)
	if attr == "" {
		t.Fatal("no fallback badge in the aside on /broken/load (the contained failure fell back to the Default)")
	}
	if !strings.Contains(attr, "default content") {
		t.Errorf("badge = %q, want the default-content label", attr)
	}
	if css == "" || css == "none" {
		t.Errorf("badge CSS content = %q, want the label to render", css)
	}

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/broken/boundary"),
		chromedp.WaitVisible(`#lab-SCREEN-BROKEN-BOUNDARY`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate broken/boundary: %v", err)
	}
	attr, css = labBadgeAttr(t, tctx)
	if attr == "" {
		t.Fatal("no fallback badge in the aside on /broken/boundary (the ErrorBoundary fallback)")
	}
	if !strings.Contains(attr, "error boundary fallback") {
		t.Errorf("badge = %q, want the error-boundary-fallback label", attr)
	}
	if css == "" || css == "none" {
		t.Errorf("badge CSS content = %q, want the label to render", css)
	}
}
