package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// staleStep is how the server answers one request: wait, then either
// refuse (400 with a field error), succeed, or fail the fetch.
// headFirst sends the status line before the wait, so fetch resolves
// early and the body arrives late.
type staleStep struct {
	wait      time.Duration
	refuse    bool
	drop      bool
	headFirst bool
}

func (s staleStep) serve(w http.ResponseWriter, r *http.Request, crossOrigin string) {
	if s.drop {
		// A redirect to another origin that sends no CORS headers fails
		// the fetch with a TypeError, the network-error path. (A closed
		// connection makes Chrome retry the POST instead.)
		time.Sleep(s.wait)
		http.Redirect(w, r, crossOrigin, http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	status, body := http.StatusOK, `{"ok":true}`
	if s.refuse {
		status, body = http.StatusBadRequest, `{"error":"validation failed","fields":{"title":["required"]},"success":false}`
	}
	if s.headFirst {
		w.WriteHeader(status)
		w.(http.Flusher).Flush()
		time.Sleep(s.wait)
	} else {
		time.Sleep(s.wait)
		w.WriteHeader(status)
	}
	fmt.Fprint(w, body)
}

// TestDispatchRPC_StaleFormResponseDropped: a form with no
// data-cui-rpc-signal has no abort controller, so two quick submits
// race. Whatever the older request does after the newer one starts, it
// must not touch the page: no errors painted over a newer success, no
// errors cleared after a newer refusal, no stale result for the leave
// guard, no reset of what the user typed since, no error toast, and no
// busy state dropped while the newer request is still out. Each case pins one of the guards in rpc.js.
func TestDispatchRPC_StaleFormResponseDropped(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	const slowModule = 600 * time.Millisecond
	ms := time.Millisecond
	for _, tc := range []struct {
		name string
		// first and second answer the two submits; gap is how long after
		// the first the second is sent.
		first, second staleStep
		gap           time.Duration
		formAttrs     string
		slowErrors    bool          // serve formerrors after slowModule
		probeAt       time.Duration // when to read the page
		probe         string        // JS expression that must be true
	}{
		{name: "late refusal", first: staleStep{wait: 700 * ms, refuse: true}, gap: 100 * ms, probeAt: 1500 * ms,
			probe: `document.getElementById('title').getAttribute('aria-invalid') !== 'true'`},
		{name: "late success", first: staleStep{wait: 700 * ms}, second: staleStep{refuse: true}, gap: 100 * ms, probeAt: 1500 * ms,
			probe: `document.getElementById('title').getAttribute('aria-invalid') === 'true'`},
		// Only the newer success may tell the leave guard how the form
		// stands: a stale {ok:false} would mark it unsaved again.
		{name: "late refusal body", first: staleStep{wait: 700 * ms, refuse: true, headFirst: true}, gap: 300 * ms, probeAt: 1500 * ms,
			probe: `document.getElementById('title').getAttribute('aria-invalid') !== 'true' && window.__results.join() === 'true'`},
		{name: "late success body", first: staleStep{wait: 700 * ms, headFirst: true}, second: staleStep{refuse: true}, gap: 300 * ms,
			formAttrs: ` data-cui-rpc-reset`, probeAt: 1500 * ms,
			probe: `document.getElementById('title').value === 'kept'`},
		{name: "slow errors module", first: staleStep{wait: 200 * ms, refuse: true}, gap: 400 * ms, slowErrors: true, probeAt: 1500 * ms,
			probe: `document.getElementById('title').getAttribute('aria-invalid') !== 'true'`},
		{name: "late network error", first: staleStep{wait: 700 * ms, drop: true}, gap: 100 * ms,
			formAttrs: ` data-cui-rpc-error-toast="Failed"`, probeAt: 1500 * ms,
			probe: `window.__toasts.length === 0`},
		{name: "busy until newer", first: staleStep{wait: 300 * ms}, second: staleStep{wait: 1200 * ms}, gap: 100 * ms, probeAt: 700 * ms,
			probe: `document.getElementById('f').getAttribute('aria-busy') === 'true'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				w.Write([]byte(js))
			})
			mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
				name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
				src, ok := Module(name)
				if !ok {
					http.NotFound(w, r)
					return
				}
				if name == "formerrors" && tc.slowErrors {
					time.Sleep(slowModule)
				}
				w.Header().Set("Content-Type", "application/javascript")
				w.Write([]byte(src))
			})
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
			t.Cleanup(other.Close)
			mux.HandleFunc("/rpc/save", func(w http.ResponseWriter, r *http.Request) {
				if hits.Add(1) == 1 {
					tc.first.serve(w, r, other.URL)
					return
				}
				tc.second.serve(w, r, other.URL)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<!doctype html><html><head><title>stale</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST"`+tc.formAttrs+`>
  <div class="fui-field" data-hui-field="" data-cui-comp="ui-form-field">
    <label class="fui-field__label" for="title">Title</label>
    <input class="fui-input" id="title" name="title" value="">
  </div>
  <button type="submit">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			submit := fmt.Sprintf(`(() => {
  window.__toasts = [];
  window.__results = [];
  document.addEventListener('gofastr:formresult', (e) => window.__results.push(e.detail.ok));
  window.__gofastr._toastOrFallback = (o) => window.__toasts.push(o);
  const f = document.getElementById('f');
  f.requestSubmit();
  setTimeout(() => { document.getElementById('title').value = 'kept'; f.requestSubmit(); }, %d);
})()`, tc.gap.Milliseconds())
			var ok bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#f`, chromedp.ByID),
				chromedp.Evaluate(submit, nil),
				chromedp.Sleep(tc.probeAt),
				chromedp.Evaluate(tc.probe, &ok),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if !ok {
				t.Errorf("after %v: %s is false", tc.probeAt, tc.probe)
			}
		})
	}
}
