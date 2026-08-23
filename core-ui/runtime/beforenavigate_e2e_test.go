package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Pages for the gofastr:beforenavigate contract (#217): two known
// routes, #go is a link the router intercepts (different path plus a
// fragment), #selfhash differs from the current URL by fragment only,
// which the router does NOT intercept (native hash behavior).
func beforeNavigatePage() string {
	return `<!doctype html><html><head><title>beforenavigate</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/other"}]</script>
</head><body>
  <nav aria-label="Primary">
    <a id="selfhash" href="/#top">Top</a>
    <a id="go" href="/other#section">Other</a>
    <a id="upper" href="/other" target="_SELF">Other, shouting</a>
    <a id="blank" href="/other" target="_blank">Other, new tab</a>
  </nav>
  <main>home screen</main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`
}

func beforeNavigateServer(t *testing.T, spaFetches *atomic.Int32) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			spaFetches.Add(1)
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "other")
			fmt.Fprint(w, `<p>other screen</p>`)
			return
		}
		fmt.Fprint(w, beforeNavigatePage())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestBeforeNavigateCancelStopsRouter: a listener that cancels
// gofastr:beforenavigate claims the click. The router must not touch
// the URL, must not fetch the target partial, and must still suppress
// the browser default so the cancelled click is not a hard page load.
func TestBeforeNavigateCancelStopsRouter(t *testing.T) {
	var fetches atomic.Int32
	srv := beforeNavigateServer(t, &fetches)
	ctx := newSeedBrowserCtx(t)

	var path, stamp string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__bnStamp = 'alive';
			document.addEventListener('gofastr:beforenavigate', function (e) { e.preventDefault(); });`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`window.__bnStamp || 'gone'`, &stamp),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if path != "/" {
		t.Errorf("cancelled gofastr:beforenavigate must leave the URL alone, at %q", path)
	}
	if n := fetches.Load(); n != 0 {
		t.Errorf("cancelled gofastr:beforenavigate must not fetch the target partial, got %d SPA fetches", n)
	}
	if stamp != "alive" {
		t.Error("cancelled gofastr:beforenavigate must not become a hard page load — window stamp was wiped")
	}
}

// TestBeforeNavigateDetailProceeds: without cancellation the event
// fires on the anchor with href/path/hash/anchor in detail, and the
// SPA navigation proceeds exactly as before.
func TestBeforeNavigateDetailProceeds(t *testing.T) {
	var fetches atomic.Int32
	srv := beforeNavigateServer(t, &fetches)
	ctx := newSeedBrowserCtx(t)

	var detail, path string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__bn = [];
			document.addEventListener('gofastr:beforenavigate', function (e) {
				window.__bn.push({
					href: e.detail.href, path: e.detail.path, hash: e.detail.hash,
					anchorId: e.detail.anchor && e.detail.anchor.id,
					bubbles: e.bubbles, cancelable: e.cancelable,
				});
			});`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify(window.__bn[0] || null)`, &detail),
		chromedp.Evaluate(`location.pathname + location.hash`, &path),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if detail == "" || detail == "null" {
		t.Fatal("gofastr:beforenavigate never fired for a click the router intercepts")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(detail), &got); err != nil {
		t.Fatalf("detail did not parse: %v (%s)", err, detail)
	}
	for k, w := range map[string]string{
		"href":     "/other#section",
		"path":     "/other",
		"hash":     "#section",
		"anchorId": "go",
	} {
		if got[k] != w {
			t.Errorf("detail.%s = %v, want %q", k, got[k], w)
		}
	}
	if got["bubbles"] != true || got["cancelable"] != true {
		t.Errorf("event must bubble and be cancelable, got bubbles=%v cancelable=%v", got["bubbles"], got["cancelable"])
	}
	if path != "/other#section" {
		t.Errorf("uncancelled gofastr:beforenavigate must let the navigation proceed, at %q", path)
	}
	if n := fetches.Load(); n < 1 {
		t.Error("uncancelled navigation must fetch the target partial — test is vacuous")
	}
}

// TestBeforeNavigateSkipsSamePathFragment: a link whose only diff
// from the current URL is the #fragment is NOT intercepted, so
// gofastr:beforenavigate must not fire for it; native hash behavior
// applies (same-document navigation, no reload).
func TestBeforeNavigateSkipsSamePathFragment(t *testing.T) {
	var fetches atomic.Int32
	srv := beforeNavigateServer(t, &fetches)
	ctx := newSeedBrowserCtx(t)

	var seen, path, stamp string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__bnStamp = 'alive';
			document.addEventListener('gofastr:beforenavigate', function () {
				window.__bnSeen = (window.__bnSeen || 0) + 1;
			});`, nil),
		chromedp.Click(`#selfhash`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`String(window.__bnSeen || 0)`, &seen),
		chromedp.Evaluate(`location.pathname + location.hash`, &path),
		chromedp.Evaluate(`window.__bnStamp || 'gone'`, &stamp),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if seen != "0" {
		t.Errorf("gofastr:beforenavigate fired %s time(s) for a same-path #fragment click the router does not intercept", seen)
	}
	if path != "/#top" {
		t.Errorf("same-path #fragment click must fall through to native hash behavior, at %q", path)
	}
	if stamp != "alive" {
		t.Error("same-path #fragment click must stay a same-document navigation — window stamp was wiped")
	}
}

// TestUppercaseSelfTargetIsSoftNavigated: HTML matches the underscore
// target keywords ASCII-case-insensitively, so `target="_SELF"` names
// the current browsing context exactly as `_self` does. Comparing the
// attribute raw sent it down the skip path, and a link that should have
// been a soft navigation became a full page load.
//
// The window stamp is the assertion that matters: a full load wipes it,
// so it distinguishes "the router handled this" from "the browser did,
// and happened to land on the same URL".
func TestUppercaseSelfTargetIsSoftNavigated(t *testing.T) {
	var fetches atomic.Int32
	srv := beforeNavigateServer(t, &fetches)
	ctx := newSeedBrowserCtx(t)

	var path, stamp string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__upStamp = 'alive'`, nil),
		chromedp.Click(`#upper`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`window.__upStamp || 'gone'`, &stamp),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if path != "/other" {
		t.Errorf("target=\"_SELF\" did not navigate, at %q", path)
	}
	if stamp != "alive" {
		t.Error(`target="_SELF" became a full page load; the underscore keywords match case-insensitively, so it is _self and the router owns it`)
	}
	if n := fetches.Load(); n < 1 {
		t.Error("no SPA partial was fetched — the router did not take the click")
	}
}

// The other half, and the reason the guard is not simply deleted: a real
// non-_self target must still fall through to the browser. Without this,
// a fix for the case above could quietly hijack every target.
func TestBlankTargetStillEscapesTheRouter(t *testing.T) {
	var fetches atomic.Int32
	srv := beforeNavigateServer(t, &fetches)
	ctx := newSeedBrowserCtx(t)

	var path, fired string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__bnFired = 'no';
			document.addEventListener('gofastr:beforenavigate', function () { window.__bnFired = 'yes'; });`, nil),
		chromedp.Click(`#blank`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`window.__bnFired`, &fired),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if path != "/" {
		t.Errorf("target=\"_blank\" must not be soft-navigated in place, at %q", path)
	}
	if fired != "no" {
		t.Error(`gofastr:beforenavigate fired for a target="_blank" click; the router must not claim it`)
	}
	if n := fetches.Load(); n != 0 {
		t.Errorf("target=\"_blank\" must not fetch a partial, got %d", n)
	}
}
