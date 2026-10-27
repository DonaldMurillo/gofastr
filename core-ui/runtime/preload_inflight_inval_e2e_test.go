package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// A prefetch still in flight when a mutation invalidates its path holds
// the pre-mutation page. Kept, the click painted it with no refetch: the
// invalidation had swept only the finished entries. The server holds the
// prefetch response until the mutation's invalidation has run.
func TestInflightPrefetchInvalidated(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var version atomic.Int64
	version.Store(1)
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})

	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/hub","layouts":["l:site"]},` +
		`{"path":"/orders","layouts":["l:site"],"preload":"hover"}` +
		`]</script>`
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` + routes +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	orders := func(v int64) string { return fmt.Sprintf(`<h1 id="orders-screen">orders v%d</h1>`, v) }

	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/hub", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<h1 id="hub-screen">Hub</h1><a id="to-orders" href="/orders">Orders</a>`))
	})
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		// The body reflects the state when the request arrived.
		body := orders(version.Load())
		if r.Header.Get("X-Gofastr-Prefetch") == "1" {
			select {
			case arrived <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "Orders")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		fmt.Fprint(w, body)
	})
	mux.HandleFunc("/mutate", func(w http.ResponseWriter, _ *http.Request) {
		version.Store(2)
		w.Header().Set("X-Gofastr-Invalidate", `["/orders"]`)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	wait := func(ch <-chan struct{}, what string) chromedp.ActionFunc {
		return func(context.Context) error {
			select {
			case <-ch:
				return nil
			case <-time.After(10 * time.Second):
				return fmt.Errorf("timed out waiting for %s", what)
			}
		}
	}
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond), // preload module load
		chromedp.Evaluate(`document.getElementById('to-orders').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		wait(arrived, "the prefetch"),
		// The mutation answers while the prefetch is still held.
		chromedp.Evaluate(`fetch('/mutate', {method: 'POST'}).then((r) => { __gofastr._inval(r); return 1; })`, nil, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		chromedp.ActionFunc(func(context.Context) error { close(release); return nil }),
		chromedp.Sleep(400*time.Millisecond), // the old prefetch completes
		chromedp.Click(`#to-orders`, chromedp.ByID),
		chromedp.WaitVisible(`#orders-screen`, chromedp.ByID),
		chromedp.Text(`#orders-screen`, &got, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got != "orders v2" {
		t.Errorf("click painted %q, want %q: an invalidated in-flight prefetch was kept", got, "orders v2")
	}
}
