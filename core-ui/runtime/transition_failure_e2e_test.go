package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestTransitionFailureRestoresURL(t *testing.T) {
	for _, navigator := range []string{"plain", "envelope"} {
		t.Run(navigator, func(t *testing.T) {
			script, err := RuntimeJS()
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			handleRuntimeModules(t, mux)
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, script)
			})
			mux.HandleFunc("/next", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Swap", "l:site")
				fmt.Fprint(w, `<p>NEXT</p>`)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				outlet := ""
				if navigator == "envelope" {
					outlet = `<aside data-cui-outlet="l:site#aside"></aside>`
				}
				fmt.Fprintf(w, `<!doctype html><html><head><title>Home</title><script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:site"]},{"path":"/next","layouts":["l:site"]}]</script></head><body><div data-cui-layout-key="l:site"><main id="main" data-cui-layout-slot="l:site" data-cui-vt="main">%s<p>HOME</p><a href="/next" id="go">Next</a></main></div><script src="/__gofastr/runtime.js"></script></body></html>`, outlet)
			})
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, `!!window.__gofastr.loadedModules?.transition`) {
				t.Fatal("transition module did not load")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				if (!document.startViewTransition) throw new Error('view transitions unavailable');
				const descriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'innerHTML');
				Object.defineProperty(document.getElementById('main'), 'innerHTML', {
					get() { return descriptor.get.call(this); },
					set() { window.__applyThrew = true; throw new Error('apply failed'); }
				});
			})()`, nil), chromedp.Click(`#go`, chromedp.ByID), chromedp.Poll(`window.__applyThrew === true`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
				t.Fatal(err)
			}
			if err := chromedp.Run(ctx, chromedp.Poll(`location.pathname === '/' && !!document.getElementById('cui-nav-toast') && !document.documentElement.hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
				t.Fatalf("failed transition did not restore URL and show failure: %v", err)
			}
		})
	}
}
