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

// A component inserted after load (an overlay, an island swap) whose
// compiled action script this page has not loaded yet: the actionloader
// module fetches it and fires the component's mount once it registered,
// as it does for a navigated page. A script that fails to load is asked
// for again the next time the component appears, and an id that names a
// prototype member (constructor) is never treated as a manifest entry.
func TestActionLoaderLoadsInsertedComponentScript(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var lateHits, ctorHits atomic.Int32
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/widget/late-comp.js", func(w http.ResponseWriter, _ *http.Request) {
		if lateHits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `window.__gofastr.register("late-comp", {
  "fill": () => { for (const o of document.querySelectorAll('[data-component="late-comp"] .out')) o.textContent = 'mounted'; }
});`)
	})
	mux.HandleFunc("/__gofastr/widget/constructor.js", func(w http.ResponseWriter, _ *http.Request) {
		ctorHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><script>window.__gofastr_actions = {"late-comp": "h1"};</script></head>
<body><div id="host"></div><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	insert := `document.getElementById('host').innerHTML = '<div data-component="constructor"></div><div data-component="late-comp"><i data-action-mount="fill"></i><span class="out">idle</span></div>'; 0`
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.actionloader)`, nil),
		chromedp.Evaluate(insert, nil),
	); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for lateHits.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if lateHits.Load() < 1 {
		t.Fatal("the inserted component's action script was never requested")
	}
	time.Sleep(300 * time.Millisecond)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('host').innerHTML = ''; 0`, nil),
		chromedp.Evaluate(insert, nil),
		chromedp.Poll(`document.querySelector('[data-component="late-comp"] .out').textContent === 'mounted'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("after a failed load the script was not asked for again or the mount never fired (%d requests): %v", lateHits.Load(), err)
	}
	if n := ctorHits.Load(); n != 0 {
		t.Errorf("data-component=\"constructor\" requested its script %d times: a prototype member read as a manifest entry", n)
	}
}
