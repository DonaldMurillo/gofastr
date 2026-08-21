package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// stateSite serves one route whose page declares a pane deep-link param,
// counting every request so tests can assert zero-fetch history moves.
func stateSite(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int64
	page := `<!doctype html><html><head><title>t</title>` +
		`<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/list","layouts":["l:site"]}]</script>` +
		`</head><body><div data-fui-layout="site" data-fui-layout-key="l:site">` +
		`<main role="main" tabindex="-1" data-fui-layout-slot="l:site">` +
		`<h1 id="list-screen">List</h1>` +
		`<div id="host" data-fui-pane-deeplink="pane"></div>` +
		`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "List")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, `<h1 id="list-screen">List</h1><div id="host" data-fui-pane-deeplink="pane"></div>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &count
}

// Back across a deep-link push (only a stateful param differs) must not
// refetch the screen, refetching is what discarded the mounted widget
// and made Forward unusable (the v0.44.0 known issue).
func TestStatefulQueryBackForwardZeroFetch(t *testing.T) {
	srv, count := stateSite(t)
	ctx := newSeedBrowserCtx(t)

	var stampKept bool
	var afterBack, afterForward string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#list-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('list-screen').dataset.stamp = 'kept'`, nil),
		// The pane/widget modules record in-page state via the router's
		// choke point; drive it the way they do.
		chromedp.Evaluate(`__gofastr._pushURL('/list?pane=secondary:41')`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`location.search`, &afterBack),
		chromedp.Evaluate(`history.forward()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`location.search`, &afterForward),
		chromedp.Evaluate(`document.getElementById('list-screen').dataset.stamp === 'kept'`, &stampKept),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if afterBack != "" {
		t.Errorf("back did not strip the stateful param: %q", afterBack)
	}
	if afterForward != "?pane=secondary:41" {
		t.Errorf("forward did not restore the stateful param: %q", afterForward)
	}
	if !stampKept {
		t.Error("screen content was rebuilt on a stateful-only history move")
	}
	if got := count.Load(); got != 1 {
		t.Errorf("stateful back/forward fetched the screen (%d requests, want 1)", got)
	}
}

// A non-stateful query diff (?p=2, pagination pushed via push-state) IS
// screen identity: back must refetch (or cache-replay) as before.
func TestIdentityQueryBackRefetches(t *testing.T) {
	srv, count := stateSite(t)
	ctx := newSeedBrowserCtx(t)

	var stamp bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#list-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('list-screen').dataset.stamp = 'kept'`, nil),
		chromedp.Evaluate(`__gofastr._pushURL('/list?p=2')`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('list-screen').dataset.stamp === 'kept'`, &stamp),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The back over an identity param must reload the screen, from the
	// LRU cache here (the boot entry for /list), so no second network
	// request, but never a silent skip: the content is re-swapped and
	// the stamp is gone.
	if stamp {
		t.Error("identity-query back did not re-render the screen")
	}
	if got := count.Load(); got != 1 {
		t.Errorf("identity back should replay the cached screen (%d requests, want 1)", got)
	}
}
