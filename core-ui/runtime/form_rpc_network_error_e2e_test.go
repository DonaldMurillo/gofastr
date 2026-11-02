package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestDispatchRPC_FormNetworkErrorIsNotSilent: a data-cui-rpc form whose
// fetch fails (offline, a dropped connection) said nothing at all unless
// the form opted into data-cui-rpc-error-toast: no toast, no field
// error, no form result, the button simply did nothing. Meridian's
// "New Customer" form, submitted offline, showed exactly that. A
// refusal already reports through the field errors and a redirect
// through a toast; a network failure toasts too ("Request failed",
// "Network error: please try again"), and the leave guard hears that
// the edits were not saved.
func TestDispatchRPC_FormNetworkErrorIsNotSilent(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, formAttrs, title string
	}{
		{name: "no opt-in", title: "Request failed"},
		{name: "opt-in title", formAttrs: ` data-cui-rpc-error-toast="Save failed"`, title: "Save failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				w.Write([]byte(js))
			})
			mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
				src, ok := Module(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js"))
				if !ok {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/javascript")
				w.Write([]byte(src))
			})
			other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			t.Cleanup(other.Close)
			// The network-error path (see staleStep.drop).
			mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
				staleStep{drop: true}.serve(w, r, other.URL)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<!doctype html><html><head><title>net</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST"`+tc.formAttrs+`>
  <input id="title" name="title" value="x">
  <button type="submit">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var got string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#f`, chromedp.ByID),
				chromedp.Evaluate(`(() => {
  window.__toasts = [];
  window.__results = [];
  document.addEventListener('gofastr:formresult', (e) => window.__results.push(e.detail.ok));
  window.__gofastr._toastOrFallback = (o) => window.__toasts.push(o);
  document.getElementById('f').requestSubmit();
})()`, nil),
				chromedp.Sleep(1500*time.Millisecond),
				chromedp.Evaluate(`JSON.stringify({toasts: window.__toasts.map(o => [o.variant, o.title]), results: window.__results})`, &got),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			want := fmt.Sprintf(`{"toasts":[["error",%q]],"results":[false]}`, tc.title)
			if got != want {
				t.Errorf("after a failed fetch:\n got %s\nwant %s", got, want)
			}
		})
	}
}
