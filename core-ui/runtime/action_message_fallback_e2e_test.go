package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// An action's answer carries its message as a string, which the kernel
// hands to _toastOrFallback. NS.toast turns a string into a title, but
// the inline fallback dropped anything without .title, so the message
// vanished exactly when the fallback was needed: the feedback module
// failed to load, or the page had no toast stack for it to render into.
func TestStringActionMessageFallsBack(t *testing.T) {
	for _, arm := range []struct {
		name          string
		moduleFailing bool
	}{
		{name: "module unavailable", moduleFailing: true},
		{name: "no toast stack"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			js, err := RuntimeJS()
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			handleRuntimeModules(t, mux)
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				w.Write([]byte(js))
			})
			mux.HandleFunc("/__gofastr/action", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"ok","message":"Order saved"}`)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, `<!doctype html><html><head>`+
					`<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>`+
					`</head><body><main role="main" tabindex="-1" id="m">m</main>`+
					`<script src="/__gofastr/runtime.js"></script></body></html>`)
			})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if arm.moduleFailing && strings.HasSuffix(r.URL.Path, "/headless-feedback.js") {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				mux.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)

			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var answer string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#m`, chromedp.ByID),
				chromedp.Evaluate(`__gofastr._serverActionFor('orders', 'save').then((result) => result && result.message)`, &answer, func(params *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
					return params.WithAwaitPromise(true)
				}),
				chromedp.Poll(`!!document.querySelector('[data-cui-toast-fallback]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
			); err != nil {
				t.Fatalf("no fallback notice for the action's string message: %v", err)
			}
			var text string
			var loaded bool
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelector('[data-cui-toast-fallback]').textContent`, &text),
				chromedp.Evaluate(`!!(__gofastr.loadedModules || {})['headless-feedback']`, &loaded),
			); err != nil {
				t.Fatal(err)
			}
			if loaded == arm.moduleFailing {
				t.Fatalf("feedback module loaded = %v, want %v: the arm did not run", loaded, !arm.moduleFailing)
			}
			if answer != "Order saved" || !strings.Contains(text, "Order saved") {
				t.Fatalf("action answered %q; fallback region reads %q, want the message", answer, text)
			}
		})
	}
}
