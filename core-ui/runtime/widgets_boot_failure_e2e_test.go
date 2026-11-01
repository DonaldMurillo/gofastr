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

// When the widgets module fails to load at boot, the catalog boot path
// must settle and stop. It used to re-arm a zero-delay timer until
// mountWidget appeared, which never happens: a 4 ms timer spun for the
// life of the page. Both compositions carry the same boot path.
func TestWidgetsLoadFailureStopsBoot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		compose func() (string, error)
		catalog string
	}{
		{"live", RuntimeJS, "/__gofastr/widgets"},
		{"static", StaticJS, "/__gofastr/widgets.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			js, err := tc.compose()
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				w.Write([]byte(js))
			})
			mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
				// Every demand module fails, the widgets module included.
				http.Error(w, "down", http.StatusBadGateway)
			})
			mux.HandleFunc(tc.catalog, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`[{"cfg":{"name":"w"},"hidden":true}]`))
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<!doctype html><html><head><title>boot</title>
  <script>
    window.__zeroTimers = 0;
    var realST = window.setTimeout;
    window.setTimeout = function (f, d) { if (!d) window.__zeroTimers++; return realST.apply(window, arguments); };
  </script></head><body>
  <main role="main"><span id="ready">ready</span></main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			time.Sleep(1500 * time.Millisecond)
			var before, after int
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__zeroTimers`, &before)); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			time.Sleep(1000 * time.Millisecond)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__zeroTimers`, &after)); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if after-before > 5 {
				t.Fatalf("%d zero-delay timers armed in one idle second after the widgets module failed to load, want the boot path stopped", after-before)
			}
			// The catalog is data, not module code: it is kept even when the
			// module failed, so openWidget's own retry of the load finds the
			// hidden widget it is asked to open.
			var kept bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`!!(window.__gofastr._widgetCatalog && window.__gofastr._widgetCatalog.w)`, &kept)); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if !kept {
				t.Fatal("widget catalog dropped when the widgets module failed to load; a later openWidget finds nothing")
			}
		})
	}
}
