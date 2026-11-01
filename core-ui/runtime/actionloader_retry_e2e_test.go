package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A component action script whose first load fails (a 5xx, a network
// blip) must load again on the next navigation that shows the
// component. The loader marked the id loaded before the fetch and never
// cleared it, so the component's controls stayed inert for the session.
func TestActionScriptReloadsAfterAFailure(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/__gofastr/runtime/"):]
		name = name[:len(name)-len(".js")]
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/__gofastr/widget/cmp.js", func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(`window.__cmpLoaded = (window.__cmpLoaded || 0) + 1;`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>loader</title></head><body>
  <main role="main"><span id="ready">ready</span><div data-component="cmp">x</div></main>
  <script>window.__gofastr_actions = { cmp: 'h1' };</script>
  <script src="/__gofastr/runtime.js"></script>
  <script>window.__gofastr.loadModule('actionloader').then(function () { window.__ready = true; });</script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__ready === true`) {
		t.Fatal("the actionloader module never loaded")
	}
	// The first fetch answers 502; wait until the browser has given up on it.
	deadline := time.Now().Add(4 * time.Second)
	for hits.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("the component script was fetched %d times on boot, want 1", hits.Load())
	}
	time.Sleep(300 * time.Millisecond)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { root: document } }))`, nil)); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__cmpLoaded === 1`) {
		t.Fatalf("after a failed load the next navigation fetched the script %d times in total, want a second fetch that loads it", hits.Load())
	}
	var scripts int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('script[src^="/__gofastr/widget/cmp.js"]').length`, &scripts)); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if scripts != 1 {
		t.Fatalf("%d component script elements on the page, want 1: the failed one is removed", scripts)
	}
}
