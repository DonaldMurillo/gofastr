package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A prefetched page outliving its session painted with no server
// contact, and its islands, actions and SSE then answered 401. The
// prefetch answer names the seconds the session has left
// (X-Gofastr-Session-Expires-In); the preload cache drops the entry at
// that point when it comes before the TTL, so the click fetches.
func TestPrefetchDroppedAtSessionEnd(t *testing.T) {
	for _, tc := range []struct {
		name, expiresIn string
		bodyDelay, wait time.Duration
		want            string
	}{
		{"session ends first", "1", 0, 1500 * time.Millisecond, "orders fetched"},
		{"ttl ends first", "600", 0, 1500 * time.Millisecond, "orders prefetched"},
		// The countdown starts at the headers: a body arriving 1.5 s
		// later does not stretch a one-second session.
		{"slow body", "1", 1500 * time.Millisecond, 200 * time.Millisecond, "orders fetched"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clickAfterPrefetch(t, tc.expiresIn, tc.bodyDelay, tc.wait); got != tc.want {
				t.Errorf("click painted %q, want %q", got, tc.want)
			}
		})
	}
}

// clickAfterPrefetch hovers a link so the runtime prefetches it with
// the given session expiry header, its body sent bodyDelay after the
// headers, waits wait once the body is out, clicks, and returns what
// the click painted.
func clickAfterPrefetch(t *testing.T, expiresIn string, bodyDelay, wait time.Duration) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	landed := make(chan struct{}, 1)
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
		body := `<h1 id="orders-screen">orders fetched</h1>`
		if r.Header.Get("X-Gofastr-Prefetch") == "1" {
			body = `<h1 id="orders-screen">orders prefetched</h1>`
			w.Header().Set("X-Gofastr-Session-Expires-In", expiresIn)
			defer func() {
				select {
				case landed <- struct{}{}:
				default:
				}
			}()
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "Orders")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		if bodyDelay > 0 && r.Header.Get("X-Gofastr-Prefetch") == "1" {
			w.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(w).Flush()
			time.Sleep(bodyDelay)
		}
		fmt.Fprint(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond), // preload module load
		chromedp.Evaluate(`document.getElementById('to-orders').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.ActionFunc(func(context.Context) error {
			select {
			case <-landed:
				return nil
			case <-time.After(10 * time.Second):
				return fmt.Errorf("timed out waiting for the prefetch")
			}
		}),
		chromedp.Sleep(wait),
		chromedp.Click(`#to-orders`, chromedp.ByID),
		chromedp.WaitVisible(`#orders-screen`, chromedp.ByID),
		chromedp.Text(`#orders-screen`, &got, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return got
}
