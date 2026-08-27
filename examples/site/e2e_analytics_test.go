package main

// Browser-level (chromedp) e2e for the first-party analytics pattern:
// battery/relay + uihost.ScriptHandler/ScriptURL/RegisterExternalScript +
// the handler.GetUser identity endpoint, all wired through the env-gated
// analytics_demo.go seam. The "vendor" is an httptest server speaking a
// PostHog-shaped protocol — zero vendor accounts, zero network egress
// beyond loopback.
//
// What each test pins:
//
//   - InitialLoad: the SDK and the initial $pageview both arrive at the
//     vendor THROUGH the relay (server-side recorded requests) while the
//     browser's own network log never leaves the app origin, under the
//     strict default CSP (pinned byte-for-byte), with zero console errors
//     or CSP violations.
//   - SPANav: one real client-side navigation (data-fui router) produces
//     exactly one more $pageview carrying the destination path, and the
//     SDK is NOT re-fetched (SPA partial responses carry no script tags).
//   - CancelledNav: a listener that preventDefault()s gofastr:beforenavigate
//     claims the click; no pageview fires and the URL stays put.
//   - CredentialStripping: with a cookie held on the app origin, the
//     browser demonstrably sends it on the same-origin relay beacon while
//     the vendor never sees Cookie/Authorization (inbound stripping) and
//     the vendor's Set-Cookie never reaches the browser (outbound).
//   - IdentityEndpoint: plain HTTP shape of whoami — application/json,
//     no-store, {"id":null} anonymous.
//
// Gated by -short like every chromedp e2e in this package; run serialized
// (the shared browser is single-tenant).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// wantDefaultCSP is the framework's strict default Content-Security-Policy
// (core/middleware security.go), the one the analytics integration must
// live under without any widening.
const wantDefaultCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'self'"

// fakeVendorSDKJS is the tiny "vendor SDK" served by the fake upstream
// THROUGH the relay. PostHog-shaped surface: window.__fakeVendor with
// capture(name, props) POSTing JSON to the configured events URL, plus
// identify/reset recording their calls. It deliberately uses default
// fetch() credentials ("same-origin"), so the app-origin cookie jar rides
// the beacon — exactly what the relay must strip.
const fakeVendorSDKJS = `(function () {
  'use strict';
  var cfg = window.__fakeVendorCfg || {};
  var eventsURL = cfg.eventsURL || '/__gofastr/t/e/';
  var calls = [];
  window.__fakeVendor = {
    capture: function (name, props) {
      calls.push({ op: 'capture', name: name, props: props || {} });
      fetch(eventsURL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          event: name, props: props || {}, url: location.href, ts: Date.now()
        }),
      }).catch(function () {});
    },
    identify: function (id) { calls.push({ op: 'identify', id: id }); },
    reset: function () { calls.push({ op: 'reset' }); },
    _calls: calls,
  };
})();
`

// fakeVendorReq is one request the fake vendor upstream received.
type fakeVendorReq struct {
	Method string
	URI    string // path?query as the upstream saw it
	Header http.Header
	Body   string
}

// fakeVendor is the fake vendor upstream: records every request and serves
// the SDK + event endpoints the relay points at.
type fakeVendor struct {
	mu   sync.Mutex
	reqs []fakeVendorReq
	srv  *httptest.Server
}

func newFakeVendor(t *testing.T) *fakeVendor {
	t.Helper()
	f := &fakeVendor{}
	mux := http.NewServeMux()
	// SDK: any /js/... path serves the fake SDK (the relay maps the tail
	// verbatim; the bootstrap requests /js/sdk.js).
	mux.HandleFunc("/js/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, "")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		// CacheOK route: the relay passes upstream cache headers through.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fmt.Fprint(w, fakeVendorSDKJS)
	})
	// Events beacon endpoint: PostHog-style 200 {"status":1}. It also
	// sets a cookie the relay must strip on the way back out.
	mux.HandleFunc("/e/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		f.record(r, string(body))
		http.SetCookie(w, &http.Cookie{Name: "vendor_sid", Value: "leak-attempt"})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":1}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeVendor) record(r *http.Request, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, fakeVendorReq{
		Method: r.Method,
		URI:    r.URL.RequestURI(),
		Header: r.Header.Clone(),
		Body:   body,
	})
}

func (f *fakeVendor) requests() []fakeVendorReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeVendorReq(nil), f.reqs...)
}

// pageviews returns the paths of every $pageview event received, in order.
func (f *fakeVendor) pageviews() []string {
	var paths []string
	for _, q := range f.requests() {
		if q.Method != http.MethodPost || !strings.HasPrefix(q.URI, "/e/") {
			continue
		}
		var ev struct {
			Event string `json:"event"`
			Props struct {
				Path string `json:"path"`
			} `json:"props"`
		}
		if err := json.Unmarshal([]byte(q.Body), &ev); err != nil || ev.Event != "$pageview" {
			continue
		}
		paths = append(paths, ev.Props.Path)
	}
	return paths
}

// analyticsE2EServer boots the site with the fake analytics wiring enabled
// (analytics_demo.go) pointed at a fresh fake vendor, and returns the
// vendor plus the app's base URL. Each call is its own app + upstream, so
// tests never share recorded-request state.
func analyticsE2EServer(t *testing.T) (*fakeVendor, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	upstream := newFakeVendor(t)
	t.Setenv("SITE_ANALYTICS_FAKE", "1")
	t.Setenv("SITE_ANALYTICS_FAKE_UPSTREAM", upstream.srv.URL)
	base := siteE2EServer(t)
	return upstream, base
}

// waitForAnalytics polls cond until true or the deadline passes, then
// fatals with desc.
func waitForAnalytics(t *testing.T, desc string, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// netRecorded is one browser-side request (or response) captured from CDP
// network events, with ExtraInfo headers merged over the base set (that is
// where the network layer reports the attached Cookie header).
type netRecorded struct {
	URL    string
	Method string
	Header http.Header
}

// netRecorder captures every request the browser makes, to prove the page
// never leaves the app origin and to inspect what the browser really sent
// on the relay beacon.
type netRecorder struct {
	origin string
	mu     sync.Mutex
	reqs   []netRecorded
	resps  []netRecorded
	// requestID → index into reqs, for merging ExtraInfo headers.
	byID map[network.RequestID]int
	// ExtraInfo events CDP delivered BEFORE their base event — the
	// ordering is not guaranteed, and dropping an early ExtraInfo
	// silently loses the Cookie header the credential test asserts on.
	pendingExtra map[network.RequestID]http.Header
}

func (n *netRecorder) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			n.mu.Lock()
			if n.byID == nil {
				n.byID = map[network.RequestID]int{}
			}
			n.byID[e.RequestID] = len(n.reqs)
			rec := netRecorded{
				URL:    e.Request.URL,
				Method: e.Request.Method,
				Header: headerFromCDP(e.Request.Headers),
			}
			if extra, ok := n.pendingExtra[e.RequestID]; ok {
				for k, v := range extra {
					rec.Header[k] = v
				}
				delete(n.pendingExtra, e.RequestID)
			}
			n.reqs = append(n.reqs, rec)
			n.mu.Unlock()
		case *network.EventRequestWillBeSentExtraInfo:
			n.mu.Lock()
			if i, ok := n.byID[e.RequestID]; ok {
				for k, v := range headerFromCDP(e.Headers) {
					n.reqs[i].Header[k] = v
				}
			} else {
				if n.pendingExtra == nil {
					n.pendingExtra = map[network.RequestID]http.Header{}
				}
				n.pendingExtra[e.RequestID] = headerFromCDP(e.Headers)
			}
			n.mu.Unlock()
		case *network.EventResponseReceived:
			n.mu.Lock()
			n.resps = append(n.resps, netRecorded{
				URL:    e.Response.URL,
				Header: headerFromCDP(e.Response.Headers),
			})
			n.mu.Unlock()
		}
	})
}

func headerFromCDP(h network.Headers) http.Header {
	out := http.Header{}
	for k, v := range h {
		out.Set(k, fmt.Sprint(v))
	}
	return out
}

func (n *netRecorder) requestsMatching(prefix string) []netRecorded {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []netRecorded
	for _, r := range n.reqs {
		if strings.HasPrefix(r.URL, prefix) {
			// Deep-copy the header map: the CDP listener keeps writing
			// into the recorded entry (ExtraInfo events) under the lock,
			// and callers read the result after the lock is released.
			cp := r
			cp.Header = r.Header.Clone()
			out = append(out, cp)
		}
	}
	return out
}

func (n *netRecorder) responseMatching(prefix string) *netRecorded {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := range n.resps {
		if strings.HasPrefix(n.resps[i].URL, prefix) {
			// Copy out (headers included): the listener keeps mutating
			// the slice entry under the lock after we return.
			cp := n.resps[i]
			cp.Header = cp.Header.Clone()
			return &cp
		}
	}
	return nil
}

// offOrigin returns every http(s) request URL that left the app origin.
func (n *netRecorder) offOrigin() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for _, r := range n.reqs {
		if strings.HasPrefix(r.URL, "http") && !strings.HasPrefix(r.URL, n.origin) {
			out = append(out, r.URL)
		}
	}
	return out
}

// assertDefaultCSP fetches url over plain HTTP and asserts the response
// carries the framework's strict default Content-Security-Policy.
func assertDefaultCSP(t *testing.T, url string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Security-Policy"); got != wantDefaultCSP {
		t.Errorf("Content-Security-Policy = %q, want the framework default %q", got, wantDefaultCSP)
	}
}

// TestE2E_Analytics_InitialLoad: full first paint under the strict default
// CSP — the SDK loads and the initial pageview fires, both through the
// relay (the vendor's own log proves it) while the browser never leaves
// the app origin, and the console/CSP error sink stays empty.
func TestE2E_Analytics_InitialLoad(t *testing.T) {
	upstream, base := analyticsE2EServer(t)
	ctx := siteBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)
	net := &netRecorder{origin: base}
	net.listen(ctx)

	// Keystone precondition: the strict default CSP is active on the page.
	assertDefaultCSP(t, base+"/")

	if err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// SDK load → whoami → initial pageview, in that order.
	waitForAnalytics(t, "initial $pageview", 10*time.Second, func() bool {
		return len(upstream.pageviews()) >= 1
	})

	if got := upstream.pageviews(); len(got) != 1 || got[0] != "/" {
		t.Errorf("initial pageviews = %v, want exactly one for \"/\"", got)
	}

	// The vendor received the SDK fetch through the relay.
	sdkFetches := 0
	for _, q := range upstream.requests() {
		if q.Method == http.MethodGet && q.URI == "/js/sdk.js" {
			sdkFetches++
		}
	}
	if sdkFetches < 1 {
		t.Errorf("vendor never saw the SDK fetch through the relay (got %d GET /js/sdk.js)", sdkFetches)
	}

	// Browser-side: the SDK was requested from the app's own mount.
	if n := len(net.requestsMatching(base + "/__gofastr/t/js/sdk.js")); n != 1 {
		t.Errorf("browser made %d requests to the relayed SDK URL, want 1", n)
	}

	// Browser-side: nothing ever left the app origin.
	if off := net.offOrigin(); len(off) > 0 {
		t.Errorf("page contacted off-origin URLs (relay contract broken): %v", off)
	}

	// Keystone: zero console errors and zero CSP violations.
	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("initial load produced %d console/CSP error(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}

	t.Logf("vendor recorded %d request(s); pageviews so far: %v", len(upstream.requests()), upstream.pageviews())
}

// TestE2E_Analytics_SPANav: a real client-side navigation (click a header
// nav link, runtime swaps <main> and fires gofastr:navigate) produces
// exactly one more $pageview carrying the destination path, and does NOT
// re-fetch the SDK (SPA partial responses carry no script tags).
func TestE2E_Analytics_SPANav(t *testing.T) {
	upstream, base := analyticsE2EServer(t)
	ctx := siteBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	if err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitForAnalytics(t, "initial $pageview", 10*time.Second, func() bool {
		return len(upstream.pageviews()) >= 1
	})
	baseline := len(upstream.pageviews())

	// Click a real site link in the desktop header nav. The data-fui
	// router intercepts it, swaps the page content client-side, and fires
	// gofastr:navigate; the destination hub hero is the "DOM appeared"
	// marker.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('nav.ui-site-header__links a[href="/primitives"]').click()`, nil),
		chromedp.WaitVisible(`section.ex-hero[aria-label="Primitives"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("SPA nav to /primitives: %v", err)
	}

	var pathname string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &pathname)); err != nil {
		t.Fatal(err)
	}
	if pathname != "/primitives" {
		t.Fatalf("after nav, pathname = %q, want /primitives", pathname)
	}

	waitForAnalytics(t, "$pageview for /primitives", 10*time.Second, func() bool {
		return len(upstream.pageviews()) >= baseline+1
	})
	// Bounded stray window: a double-fire (navigate listener + something
	// else) would land here and fail the exact-count check below.
	if err := chromedp.Run(ctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	got := upstream.pageviews()
	if len(got) != baseline+1 {
		t.Errorf("pageviews after nav = %v, want exactly one more than baseline %d", got, baseline)
	}
	if len(got) > 0 && got[len(got)-1] != "/primitives" {
		t.Errorf("nav pageview path = %q, want /primitives", got[len(got)-1])
	}
	sdkFetches := 0
	for _, q := range upstream.requests() {
		if q.Method == http.MethodGet && q.URI == "/js/sdk.js" {
			sdkFetches++
		}
	}
	if sdkFetches != 1 {
		t.Errorf("SDK fetched %d time(s) across initial load + SPA nav, want 1 (partials must not re-emit script tags)", sdkFetches)
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("SPA nav produced %d console/CSP error(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	t.Logf("pageviews: %v (baseline %d)", upstream.pageviews(), baseline)
}

// TestE2E_Analytics_CancelledNav: a page-injected listener that
// preventDefault()s gofastr:beforenavigate for one link claims the click —
// no navigation, no SPA fetch, and NO new pageview.
func TestE2E_Analytics_CancelledNav(t *testing.T) {
	upstream, base := analyticsE2EServer(t)
	ctx := siteBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	if err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Cancel exactly one link's navigation. __bnCancelled is the
		// non-vacuity probe: the router only fires beforenavigate for
		// clicks it is about to intercept.
		chromedp.Evaluate(`window.__bnCancelled = false;
			document.addEventListener('gofastr:beforenavigate', function (e) {
				var a = e.target && e.target.closest ? e.target.closest('a') : null;
				if (a && a.getAttribute('href') === '/framework') {
					e.preventDefault();
					window.__bnCancelled = true;
				}
			});`, nil),
	); err != nil {
		t.Fatalf("navigate + inject cancel listener: %v", err)
	}
	waitForAnalytics(t, "initial $pageview", 10*time.Second, func() bool {
		return len(upstream.pageviews()) >= 1
	})
	before := len(upstream.pageviews())

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('nav.ui-site-header__links a[href="/framework"]').click()`, nil),
		// Bounded settle: a pageview that should not exist cannot be
		// waited for, so give the (suppressed) nav room to misfire.
		chromedp.Sleep(1200*time.Millisecond),
	); err != nil {
		t.Fatalf("click cancelled link: %v", err)
	}

	var pathname, cancelled string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`location.pathname`, &pathname),
		chromedp.Evaluate(`String(window.__bnCancelled)`, &cancelled),
	); err != nil {
		t.Fatal(err)
	}
	if cancelled != "true" {
		t.Fatal("gofastr:beforenavigate never fired for the click — the cancellation probe is vacuous")
	}
	if pathname != "/" {
		t.Errorf("cancelled navigation must leave the URL alone, at %q", pathname)
	}
	if got := upstream.pageviews(); len(got) != before {
		t.Errorf("cancelled navigation fired pageviews %v, want none (baseline %d)", got, before)
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("cancelled nav produced %d console/CSP error(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
}

// TestE2E_Analytics_CredentialStripping: with a cookie held on the app
// origin before the beacon fires, the browser demonstrably attaches it to
// the same-origin relay beacon (non-vacuity), while the vendor upstream
// never sees a Cookie or Authorization header on ANY request, and the
// vendor's own Set-Cookie never reaches the browser.
func TestE2E_Analytics_CredentialStripping(t *testing.T) {
	upstream, base := analyticsE2EServer(t)
	ctx := siteBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)
	net := &netRecorder{origin: base}
	net.listen(ctx)

	if err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		// Hold a credential on the app origin BEFORE any beacon fires.
		// Plain cookie (no __Host- prefix / Secure flag): the e2e origin
		// is plain http, which Chrome rejects Secure-marked cookies on.
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie("e2e_cred", "secret-token").WithURL(base).WithPath("/").Do(ctx)
		}),
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("set cookie + navigate: %v", err)
	}
	waitForAnalytics(t, "initial $pageview", 10*time.Second, func() bool {
		return len(upstream.pageviews()) >= 1
	})

	// Non-vacuity: the browser really sent the cookie on the relay beacon.
	// Poll rather than read once: the wait above gates on the SERVER
	// receiving the beacon, but the Cookie header rides the CDP
	// ExtraInfo event, and chromedp can deliver that to the recorder
	// AFTER the upstream already answered — a one-shot read here lost
	// the race twice in CI even with the recorder's out-of-order
	// buffering in place.
	waitForAnalytics(t, "beacon with app-origin cookie recorded", 10*time.Second, func() bool {
		for _, b := range net.requestsMatching(base + "/__gofastr/t/e/") {
			if strings.Contains(b.Header.Get("Cookie"), "e2e_cred") {
				return true
			}
		}
		return false
	})

	// The vendor never saw ANY credential, on any request, either direction.
	for _, q := range upstream.requests() {
		if c := q.Header.Get("Cookie"); c != "" {
			t.Errorf("vendor saw Cookie %q on %s %s (relay must strip inbound credentials)", c, q.Method, q.URI)
		}
		if a := q.Header.Get("Authorization"); a != "" {
			t.Errorf("vendor saw Authorization %q on %s %s", a, q.Method, q.URI)
		}
	}

	// Outbound direction: the vendor's Set-Cookie never reached the browser.
	if resp := net.responseMatching(base + "/__gofastr/t/e/"); resp != nil && resp.Header.Get("Set-Cookie") != "" {
		t.Errorf("relayed beacon response carried Set-Cookie %q to the browser", resp.Header.Get("Set-Cookie"))
	}
	var docCookie string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.cookie`, &docCookie)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(docCookie, "vendor_sid") {
		t.Errorf("vendor cookie leaked into document.cookie: %q", docCookie)
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("credential test produced %d console/CSP error(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	t.Logf("vendor saw %d request(s), none carrying credentials", len(upstream.requests()))
}

// TestE2E_Analytics_IdentityEndpoint: the whoami endpoint's plain-HTTP
// contract — application/json, no-store, {"id":null} for an anonymous
// visitor.
func TestE2E_Analytics_IdentityEndpoint(t *testing.T) {
	_, base := analyticsE2EServer(t)

	resp, err := http.Get(base + "/__site/analytics/whoami")
	if err != nil {
		t.Fatalf("GET whoami: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET whoami: status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var got struct {
		ID any `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode whoami body: %v", err)
	}
	if got.ID != nil {
		t.Errorf("whoami id = %v, want null (no auth chain wired on the site)", got.ID)
	}

	// Authed shape: the site app wires no auth battery and the harness has
	// no session helper, so the logged-in {"id":"..."} variant is noted as
	// skipped in the task report rather than built here.
	t.Log("authed whoami variant skipped: no auth chain on the site app, producing a logged-in session would mean building a login flow")
}

// TestE2E_Analytics_InertByDefault pins the isolation contract: with the
// env gate unset (the default site build), no analytics artifact exists —
// no bootstrap tag on the page, and none of the demo routes serve. The
// wiring is unobservable when off.
func TestE2E_Analytics_InertByDefault(t *testing.T) {
	// No t.Setenv here: the gate must stay OFF. t.Setenv from the tests
	// above has been restored, so setupServer sees a clean environment.
	base := siteE2EServer(t)

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	pageBytes, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(pageBytes), "/__site/analytics/") {
		t.Error("default build emitted the analytics bootstrap; the env gate leaked")
	}

	for _, p := range []string{
		"/__site/analytics/bootstrap.js",
		"/__site/analytics/whoami",
		"/__gofastr/t/js/sdk.js",
		"/__gofastr/t/e/batch",
	} {
		r, err := http.Get(base + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("default build must not serve %s, got %d", p, r.StatusCode)
		}
	}
}
