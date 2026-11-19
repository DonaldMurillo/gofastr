package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// serveInputTriggerRPC serves the runtime plus one HTML page whose
// data-cui-rpc carrier posts JSON to endpoint. It returns the server's
// base URL, the hit counter and the last decoded body, so each
// input-trigger test drives its own control and asserts on the recorded
// request.
func serveInputTriggerRPC(t *testing.T, endpoint, pageHTML string) (string, *atomic.Int32, *atomic.Pointer[map[string]string]) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	var last atomic.Pointer[map[string]string]
	var hits atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc(endpoint, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body := map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last.Store(&body)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, pageHTML)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, &hits, &last
}

// TestInputTrigger drives one input-triggered RPC round trip per carrier
// shape a hand-written island can take: each case serves its own page via
// serveInputTriggerRPC, fires the control, and asserts the runtime posted
// the control's `name` as the JSON key. The case comments record the
// behavior each shape exists to prove.
func TestInputTrigger(t *testing.T) {
	cases := []struct {
		name      string
		sel       string // the control waited for before driving it
		act       chromedp.Action
		endpoint  string
		pageHTML  string
		wantKey   string
		wantVal   string
		noFireWhy string // failure text when the RPC never fires
		wantWhy   string // tail of the body-mismatch failure
	}{
		{
			// A <select> change is delivered as an `input` event in every
			// modern browser, so a hand-written island can drive a
			// select-triggered RPC with the documented recipe, wrap the
			// control in
			// `<form data-cui-rpc data-cui-rpc-trigger="input">`, WITHOUT
			// any dedicated data-cui-rpc-trigger="change". This case is
			// the empirical proof behind the recipe in
			// framework/docs/content/interactive-patterns.md: it selects
			// an option and asserts the runtime posted the control's
			// `name` as the JSON key.
			//
			// debounce-ms=1 keeps the test fast; the select still rides
			// the `input` event the same as any keystroke would.
			name:     "SelectFiresRPC",
			sel:      `#cat`,
			act:      chromedp.SetValue(`#cat`, "parts", chromedp.ByID),
			endpoint: "/filter",
			pageHTML: `<!doctype html><html><head><title>select-trigger</title></head><body>
  <form data-cui-rpc="/filter" data-cui-rpc-method="POST"
        data-cui-rpc-trigger="input" data-cui-rpc-debounce-ms="1">
    <select id="cat" name="category">
      <option value="all">All</option>
      <option value="tools">Tools</option>
      <option value="parts">Parts</option>
    </select>
  </form>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`,
			wantKey:   "category",
			wantVal:   "parts",
			noFireWhy: "selecting an option did not fire the input-triggered RPC — the wrap-in-form recipe is broken",
			wantWhy:   " — the JSON key is the control's `name` attribute, not its id",
		},
		{
			// The combobox renders its RPC carrier as a <div>, not a
			// <form>, so an embedding host <form> survives HTML parsing
			// (a nested form open tag is dropped and its close tag would
			// close the host form). The runtime must still serialize the
			// carrier's named controls — a typed query has to reach the
			// handler as name=value exactly as it did when the carrier
			// was a real form. Regression test for the empty-body class
			// the SegmentedControl #194 fix documented.
			name:     "DivCarrierSendsControls",
			sel:      `#q`,
			act:      chromedp.SendKeys(`#q`, "acc", chromedp.ByID),
			endpoint: "/search",
			pageHTML: `<!doctype html><html><head><title>div-carrier</title></head><body>
  <div data-cui-rpc="/search" data-cui-rpc-method="POST"
       data-cui-rpc-trigger="input" data-cui-rpc-debounce-ms="1">
    <input id="q" name="q" type="text" autocomplete="off">
  </div>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`,
			wantKey:   "q",
			wantVal:   "acc",
			noFireWhy: "typing in a div-carrier input did not fire the RPC",
			wantWhy:   " from the div carrier's control",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, hits, last := serveInputTriggerRPC(t, tc.endpoint, tc.pageHTML)

			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				chromedp.WaitVisible(tc.sel, chromedp.ByID),
				tc.act,
				chromedp.Sleep(400*time.Millisecond),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}

			if hits.Load() == 0 {
				t.Fatal(tc.noFireWhy)
			}
			got := last.Load()
			if got == nil {
				t.Fatal("RPC fired but no body recorded")
			}
			if (*got)[tc.wantKey] != tc.wantVal {
				t.Fatalf("expected {%q:%q}%s, got %v", tc.wantKey, tc.wantVal, tc.wantWhy, *got)
			}
		})
	}
}
