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
  <!-- name="submit" shadows form.submit via HTML named-property lookup -->
  <button id="go" type="submit" name="submit" value="1">Save</button>
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

// The counterpart: a data-fui-rpc form must NOT be submitted natively when
// the module is missing. It targets a JSON API — the resource engine emits
// these with no enctype at all and rpc.js builds the body — so a native
// submit posts urlencoded (415) or cannot issue its declared PUT (405).
// Either way the browser leaves the page for a raw error and the user's
// input is gone, which is worse than staying put. Warning is the remedy.
func TestRPCFormIsNotNativelySubmittedWhenModuleMissing(t *testing.T) {
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
	// rpc.js is deliberately not served.
	mux.HandleFunc("/api/things", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":415,"error":"unsupported media type"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Exactly what resource.Config.Form emits: no enctype, a JSON
		// action, and the RPC attributes.
		fmt.Fprint(w, `<!doctype html><html><body>
<form id="f" action="/api/things" method="POST"
      data-fui-rpc="/api/things" data-fui-rpc-method="POST">
  <input id="note" name="note" value="typed by the user">
  <button id="go" type="submit">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var stillThere string
	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(1200*time.Millisecond),
		// The user must still be on the form with their input intact.
		chromedp.Evaluate(`(document.getElementById('note')||{}).value || ''`, &stillThere),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if got := posts.Load(); got != 0 {
		t.Errorf("form was submitted natively %d time(s) to a JSON endpoint — that answers 415 and navigates the user off the page", got)
	}
	if stillThere != "typed by the user" {
		t.Errorf("the user's input is gone (field = %q) — the page navigated away instead of staying put", stillThere)
	}
}
