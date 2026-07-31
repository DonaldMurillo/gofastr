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

// The bridge calls preventDefault() before awaiting src/rpc.js so a click
// that lands mid-download is not lost. The cost is that a module which
// never arrives — a host serving runtime.js but not
// /__gofastr/runtime/<name>.js, or a network blip — would swallow the
// user's submit entirely: prevented from submitting natively, never
// dispatched. When RPC was compiled into core that could not happen.
//
// A non-JSON intercepted form must therefore fall back to a native submit.
func TestFormSubmitSurvivesMissingRPCModule(t *testing.T) {
	var posts atomic.Int32

	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// Deliberately NOT serving /__gofastr/runtime/rpc.js — this is the
	// misconfigured host the fallback exists for.
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><span id="done">saved</span></body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body>
<form id="f" action="/save" method="POST" data-fui-spa
      enctype="application/x-www-form-urlencoded">
  <input name="note" value="kept">
  <button id="go" type="submit">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(1200*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if got := posts.Load(); got != 1 {
		t.Fatalf("form POSTed %d time(s), want 1 — the submit was swallowed when the RPC module failed to load", got)
	}
}
