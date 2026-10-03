package main

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// E2E coverage for the behaviour registry's first real client: the
// site-ping behaviour registered by THIS package (behavior_ping.go),
// rendered on /components/button. Mirrors the runtime-split suite's
// contract (e2e_runtime_modules_test.go): no marker no fetch, marker
// fetches, the manifest is content-addressed, SPA navigation loads it,
// hover prefetch warms it.

const (
	pingModulePath = "/__gofastr/runtime/site-ping.js"
	pingDemoPath   = "/components/button"
)

// the site-ping module, or "" when none was observed.
func fetchedModuleURL(urls *sync.Map) string {
	found := ""
	urls.Range(func(k, _ any) bool {
		if u := k.(string); strings.Contains(u, pingModulePath) {
			found = u
			return false
		}
		return true
	})
	return found
}

// listedURLs flattens the recorded runtime-module fetch URLs for
// failure messages.
func listedURLs(urls *sync.Map) []string {
	var listed []string
	urls.Range(func(k, _ any) bool {
		listed = append(listed, k.(string))
		return true
	})
	return listed
}

// The home page carries no [data-site-ping] marker, so the site's
// registered behaviour must not be fetched there.
func TestE2E_BehaviorRegistry_NoMarkerNoFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if u := fetchedModuleURL(urls); u != "" {
		t.Errorf("home page should NOT fetch the site-ping module; got %s", u)
	}
}

// /components/button carries the [data-site-ping] marker: the module is
// fetched, the button gets data-site-pinged="1", and a click toggles
// aria-pressed (the module contract's visible behaviour).
func TestE2E_BehaviorRegistry_MarkerLoadsAndAttaches(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	var attached, pressed string
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+pingDemoPath),
		chromedp.WaitVisible(`#site-ping-btn`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('site-ping-btn').getAttribute('data-site-pinged') || ''`, &attached),
		// The attached module must own the click: aria-pressed flips.
		chromedp.Click(`#site-ping-btn`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('site-ping-btn').getAttribute('aria-pressed') || ''`, &pressed),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if u := fetchedModuleURL(urls); u == "" {
		t.Errorf("%s should fetch the site-ping module; runtime urls observed: %v", pingDemoPath, listedURLs(urls))
	}
	if attached != "1" {
		t.Errorf("site-ping button not attached (data-site-pinged=%q)", attached)
	}
	if pressed != "true" {
		t.Errorf("click on site-ping button did not toggle aria-pressed (got %q)", pressed)
	}
}

// The fetched URL is content-addressed: its ?v= equals both the hash
// window.__gofastr_runtime_modules carries and the hash the Go side
// computes, and window.__gofastr_behaviors declares the marker the
// kernel scanned.
func TestE2E_BehaviorRegistry_ContentAddressed(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	var manifestV, hasBehaviors string
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+pingDemoPath),
		chromedp.WaitVisible(`#site-ping-btn`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`(window.__gofastr_runtime_modules && window.__gofastr_runtime_modules['site-ping']) || ''`, &manifestV),
		chromedp.Evaluate(`!!(window.__gofastr_behaviors && window.__gofastr_behaviors['site-ping'] && window.__gofastr_behaviors['site-ping'].s.indexOf('[data-site-ping]') >= 0) ? 'yes' : 'no'`, &hasBehaviors),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	u := fetchedModuleURL(urls)
	if u == "" {
		t.Fatalf("site-ping module never fetched; runtime urls observed: %v", listedURLs(urls))
	}
	wantV := runtime.ModuleHash("site-ping")
	if wantV == "" {
		t.Fatal("runtime.ModuleHash(site-ping) returned empty — the behaviour is not a module from the host down")
	}
	if !strings.Contains(u, "?v="+wantV) {
		t.Errorf("fetched URL %s does not carry the computed ?v=%s", u, wantV)
	}
	if manifestV != wantV {
		t.Errorf("window.__gofastr_runtime_modules['site-ping'] = %q, want %q", manifestV, wantV)
	}
	if hasBehaviors != "yes" {
		t.Errorf("window.__gofastr_behaviors does not declare site-ping with its marker (got %q)", hasBehaviors)
	}
}

// SPA navigation from the home page to the demo page loads the module
// through the post-navigation scan: a cold visit to / (no marker, no
// fetch), then a client-side nav whose swapped-in document carries the
// marker.
func TestE2E_BehaviorRegistry_SPANavLoadsBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	var attached string
	if err := chromedp.Run(ctx,
		network.Enable(),
		// Home: no marker, module must not be loaded yet (cold cache).
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Sleep(400*time.Millisecond),
		// Real client navigation: the runtime intercepts the anchor
		// click, swaps <main>, and dispatches gofastr:navigate.
		chromedp.Evaluate(`(() => {
            const a = document.createElement('a');
            a.href = '`+pingDemoPath+`';
            a.textContent = 'nav to demo';
            document.body.appendChild(a);
            a.click();
        })()`, nil),
		chromedp.WaitVisible(`#site-ping-btn`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('site-ping-btn').getAttribute('data-site-pinged') || ''`, &attached),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if u := fetchedModuleURL(urls); u == "" {
		t.Errorf("SPA navigation to %s should fetch the site-ping module; runtime urls observed: %v", pingDemoPath, listedURLs(urls))
	}
	if attached != "1" {
		t.Errorf("button in the SPA-swapped document never attached (data-site-pinged=%q)", attached)
	}
}

// Hovering the demo button's data-fui-prefetch="site-ping" (rendered on
// the demo page next to the marker) warms the module. The home page has
// no such element, so a synthetic one proves the prefetch path works
// for a registered name without waiting for the marker scan.
func TestE2E_BehaviorRegistry_HoverPrefetch(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	urls, cancel := collectRuntimeModuleURLs(ctx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const btn = document.createElement('button');
            btn.setAttribute('data-fui-prefetch', 'site-ping');
            btn.textContent = 'prefetch site-ping';
            document.body.appendChild(btn);
            btn.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }));
        })()`, nil),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if u := fetchedModuleURL(urls); u == "" {
		t.Errorf("pointerover on data-fui-prefetch=site-ping should fetch the module; runtime urls observed: %v", listedURLs(urls))
	}
}

// The section is gated to the Button page: another components page
// carries neither the marker nor the prefetch hint. A plain fetch, no
// browser needed; without this the gate could be removed unnoticed.
func TestE2E_BehaviorRegistry_OtherPagesCarryNoMarker(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	res, err := http.Get(base + "/components/datatable")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	// A marker-free body proves nothing on its own: a 404 page or a
	// redirect elsewhere has none either. The page has to be the one
	// asked for, served.
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /components/datatable returned %s", res.Status)
	}
	if got := res.Request.URL.Path; got != "/components/datatable" {
		t.Fatalf("GET /components/datatable ended at %s", got)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"data-site-ping", `data-fui-prefetch="site-ping"`, "Registered behaviour"} {
		if strings.Contains(string(body), bad) {
			t.Errorf("/components/datatable carries %q; the section is meant for the Button page only", bad)
		}
	}
}
