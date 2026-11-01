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

// debounceServer serves an input-triggered RPC form with a slow
// debounce and a submit button, counting POSTs to /save.
func debounceServer(t *testing.T, hits *atomic.Int32) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/save", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>debounce</title></head><body>
<div id="host"><form id="f" data-cui-rpc="/save" data-cui-rpc-method="POST"
      data-cui-rpc-trigger="input" data-cui-rpc-debounce-ms="500">
  <input id="q" name="q" value="">
  <button type="submit" id="go">Save</button>
</form></div>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

// typeAndWaitForTimer types into the field and waits until the rpc
// module has loaded, so the debounce timer is armed.
func typeAndWaitForTimer() chromedp.Action {
	return chromedp.Tasks{
		chromedp.SendKeys(`#q`, "abc", chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr.loadedModules || {}).rpc`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(50 * time.Millisecond),
	}
}

// A submit that lands inside the debounce window sends the form once.
// The pending input timer was left armed, so it fired after the submit
// and POSTed the same form a second time.
func TestSubmitCancelsPendingInputRPC(t *testing.T) {
	var hits atomic.Int32
	url := debounceServer(t, &hits)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		typeAndWaitForTimer(),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(1200*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("typing then submitting inside the debounce window sent %d POSTs, want 1", n)
	}
}

// A form that leaves the page inside its debounce window sends
// nothing: the timer fired against the detached form and wrote its
// stale value after the reader had moved on.
func TestDetachedFormSkipsPendingInputRPC(t *testing.T) {
	var hits atomic.Int32
	url := debounceServer(t, &hits)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		typeAndWaitForTimer(),
		chromedp.Evaluate(`document.getElementById('host').innerHTML = '<p>next page</p>'; true`, nil),
		chromedp.Sleep(1200*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("a form removed inside its debounce window sent %d POSTs, want 0", n)
	}
}
