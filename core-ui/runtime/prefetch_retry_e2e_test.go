package runtime

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A failed prefetch fetch must not pin the element as attempted. Marker-
// driven modules self-heal: every SPA nav and DOM insertion re-runs
// _scanForModules, which retries loadModule for present-but-unloaded
// markers. `tabs` deliberately has no marker entry (core bundle budget),
// so the prefetch bridge is the ONLY loader: pin the element on failure
// and a vacate strip's panels stay empty for the page lifetime. The
// bridge marks an element attempted only once its fetch succeeds, so the
// next hover/focus retries — to click a tab the pointer must cross the
// strip, which fires pointerover again.
func TestPrefetchRetriesAfterFailedFetch(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("tabs")
	if !ok {
		t.Fatal("tabs module not embedded")
	}

	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/hits", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hits":` + strconv.FormatInt(int64(hits.Load()), 10) + `}`))
	})
	// First tabs.js request fails (network error class: deploy blip,
	// transient 404); every later one succeeds.
	mux.HandleFunc("/__gofastr/runtime/tabs.js", func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(mod))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><html><head><title>prefetch-retry</title></head><body>
  <div id="wrap" data-fui-prefetch="tabs">strip</div>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, _ := runTabsContractBrowser(t)

	var loaded bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#wrap`, chromedp.ByID),
		// First hover: fetch #1 fails, the module must NOT load.
		chromedp.Evaluate(`document.getElementById('wrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`fetch('/hits').then(r => r.json()).then(j => j.hits >= 1)`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`, &loaded),
	); err != nil || !ok || loaded {
		t.Fatalf("setup: first fetch must fail without loading the module (fetched ok=%v, loaded=%v, err=%v)", ok, loaded, err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("setup: tabs.js requests = %d, want exactly 1", got)
	}

	// Second hover on the SAME element: the bridge must retry.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('wrap').dispatchEvent(
			new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.tabs)`,
			&ok, chromedp.WithPollingInterval(50*time.Millisecond)),
	); err != nil || !ok {
		t.Fatalf("a failed prefetch must be retried on the next hover of the same element (ok=%v, err=%v)", ok, err)
	}
	if got := hits.Load(); got < 2 {
		t.Fatalf("tabs.js requests = %d, want >= 2 (the retry must hit the network)", got)
	}
}
