package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Page for the activelink ownership contract (#218): a primary nav
// with an ordinary exact-match link, an opt-out link carrying a
// hand-set aria-current, and a scrollspy rail (wrap div + inner nav +
// in-page anchors, the shape core-ui/patterns/scrollspy emits) whose
// active state another module owns. The scrollspy module itself is
// inert here (no matching targets in <main>), so the aria-current
// values below stand in for what it would set while scrolling.
func activelinkSkipPage() string {
	return `<!doctype html><html><head><title>activelink</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/other"}]</script>
</head><body>
  <nav aria-label="Primary">
    <a id="home" href="/">Home</a>
    <a id="other" href="/other">Other</a>
    <a id="pinned" href="/somewhere" data-fui-activelink-skip aria-current="location" class="active">Pinned</a>
    <a id="bare" href="/elsewhere" data-fui-activelink-skip>Bare</a>
  </nav>
  <div data-fui-scrollspy="main" data-fui-scrollspy-target="section[id]">
    <nav aria-label="On this page">
      <a id="spy1" href="#s1" aria-current="true" class="active">Section one</a>
      <a id="spy2" href="#s2">Section two</a>
    </nav>
  </div>
  <nav aria-label="pagination">
    <a id="page1" href="?page=1" aria-current="page">1</a>
    <a id="page2" href="?page=2">2</a>
  </nav>
  <main>home screen <span id="ready">ready</span></main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`
}

func activelinkSkipServer(t *testing.T) *httptest.Server {
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
	// Highlighting lives in the idle-loaded activelink module; the
	// scrollspy marker demand-loads that module too.
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "other")
			fmt.Fprint(w, `<p>other screen</p>`)
			return
		}
		fmt.Fprint(w, activelinkSkipPage())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestActiveLinkSkipKeepsAuthorState: across a SPA navigation,
// data-fui-activelink-skip keeps a hand-set aria-current untouched,
// links inside a [data-fui-scrollspy] wrap keep their
// aria-current="true", and the ordinary exact-match contract still
// holds (aria-current="page" + .active move to the new path's link).
// TestActiveLinkKeepsUnmanagedAriaCurrent: a server-rendered
// aria-current="page" on a NON-matching nav link (a pagination link for
// the current page, whose href is a query-only "?page=1") is host
// content the module never stamped — no .active class — so the sweep
// must leave it alone instead of stripping the attribute.
func TestActiveLinkKeepsUnmanagedAriaCurrent(t *testing.T) {
	srv := activelinkSkipServer(t)
	ctx := newSeedBrowserCtx(t)

	var cur string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Give the idle-loaded module time for its initial pass.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('page1').getAttribute('aria-current')`, &cur),
	); err != nil {
		t.Fatal(err)
	}
	if cur != "page" {
		t.Fatalf("host-rendered pagination aria-current = %q, want %q (unmanaged links must not be stripped)", cur, "page")
	}
}

func TestActiveLinkSkipKeepsAuthorState(t *testing.T) {
	srv := activelinkSkipServer(t)
	ctx := newSeedBrowserCtx(t)

	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Let the idle-loaded activelink module run its initial pass
		// before navigating, so the test also covers its load-time
		// clearing, not just the gofastr:navigate pass.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Click(`#other`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify({
			path: location.pathname,
			pinned: document.getElementById('pinned').getAttribute('aria-current'),
			pinnedActive: document.getElementById('pinned').classList.contains('active'),
			bare: document.getElementById('bare').getAttribute('aria-current'),
			bareActive: document.getElementById('bare').classList.contains('active'),
			spy1: document.getElementById('spy1').getAttribute('aria-current'),
			spy1Active: document.getElementById('spy1').classList.contains('active'),
			other: document.getElementById('other').getAttribute('aria-current'),
			otherActive: document.getElementById('other').classList.contains('active'),
			home: document.getElementById('home').getAttribute('aria-current'),
			homeActive: document.getElementById('home').classList.contains('active'),
		})`, &state),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(state), &got); err != nil {
		t.Fatalf("state did not parse: %v (%s)", err, state)
	}
	if got["path"] != "/other" {
		t.Fatalf("SPA navigation did not reach /other, at %v — test is vacuous", got["path"])
	}
	if got["pinned"] != "location" {
		t.Errorf("data-fui-activelink-skip link lost its author-set aria-current (got %v); activelink must neither set nor clear it", got["pinned"])
	}
	// Retention, not just non-addition: activelink clears `.active` in the
	// same branch it clears aria-current, so a skip link that starts with
	// the class has to keep it. Asserting only that the class is absent
	// would pass against a module that had stripped it.
	if got["pinnedActive"] != true {
		t.Error("data-fui-activelink-skip link lost its author-set .active class")
	}
	// The other direction, on a skip link that starts with neither: the
	// module must not stamp state onto it either.
	if got["bare"] != nil || got["bareActive"] != false {
		t.Errorf("activelink stamped state onto a bare data-fui-activelink-skip link, got %v / active=%v", got["bare"], got["bareActive"])
	}
	if got["spy1"] != "true" || got["spy1Active"] != true {
		t.Errorf(`scrollspy link lost the state its own module owns, got aria-current=%v / active=%v`, got["spy1"], got["spy1Active"])
	}
	if got["other"] != "page" || got["otherActive"] != true {
		t.Errorf(`exact-match link must gain aria-current="page" + .active, got %v / active=%v`, got["other"], got["otherActive"])
	}
	if got["home"] != nil || got["homeActive"] != false {
		t.Errorf("previously-active link must lose aria-current + .active, got %v / active=%v", got["home"], got["homeActive"])
	}
}
