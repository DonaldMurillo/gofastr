package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// busyRig serves page under a /rpc that holds every request until the
// returned channel closes, counting arrivals.
func busyRig(t *testing.T, page string) (*httptest.Server, *atomic.Int32, chan struct{}) {
	t.Helper()
	hits := new(atomic.Int32)
	hold := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, MustRuntimeJS())
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-hold:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body>`+page+`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	})
	return srv, hits, hold
}

// A data-cui-rpc trigger the runtime cannot disable (a span, a link)
// sends one request per settled dispatch: a second click while the first
// is in flight is dropped, on the cold-module path and the warm one.
func TestBusyRPCTriggerSendsOnce(t *testing.T) {
	srv, hits, hold := busyRig(t, `<span id="go" data-cui-rpc="/rpc">Go</span>`)
	ctx := chromedptest.Context(t)

	twice := `(() => { const g = document.getElementById('go'); g.click(); g.click(); })()`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(twice, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("cold double click sent %d requests, want 1", n)
	}
	close(hold)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!document.getElementById('go').hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(twice, nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("warm double click: %d requests in total, want 2", n)
	}
}

// A control carries a value, so a second change while the first is in
// flight still goes out: the server must see the newest state.
func TestControlRPCSendsEveryChange(t *testing.T) {
	srv, hits, _ := busyRig(t, `<input type="checkbox" id="go" name="on" data-cui-rpc="/rpc">`)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => { const g = document.getElementById('go'); g.click(); g.click(); })()`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("two changes sent %d requests, want 2", n)
	}
}

// A data-cui-rpc form whose method is POST creates, so a re-submit while
// its own POST is in flight is dropped: a double-click plus Ctrl+S sent
// three POSTs and an entity with no unique key got three rows. Once the
// first answers, the next submit goes out. A PATCH form keeps the
// newest-value rule: every submit reaches the server.
func TestFormPOSTResubmitInFlightSendsOnce(t *testing.T) {
	thrice := `(() => { const f = document.getElementById('f'); f.requestSubmit(); f.requestSubmit(); f.requestSubmit(); })()`
	for _, tc := range []struct {
		method string
		want   int32
	}{{"POST", 1}, {"PATCH", 3}} {
		t.Run(tc.method, func(t *testing.T) {
			srv, hits, hold := busyRig(t, `<form id="f" data-cui-rpc="/rpc" data-cui-rpc-method="`+tc.method+`"><input name="title" value="x"><button type="submit">Save</button></form>`)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL),
				chromedp.Evaluate(thrice, nil),
				chromedp.Sleep(400*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			if n := hits.Load(); n != tc.want {
				t.Fatalf("three submits of a %s form sent %d requests, want %d", tc.method, n, tc.want)
			}
			if tc.method != "POST" {
				return
			}
			close(hold)
			if err := chromedp.Run(ctx,
				chromedp.Poll(`!document.getElementById('f').hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(`document.getElementById('f').requestSubmit()`, nil),
				chromedp.Sleep(300*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			if n := hits.Load(); n != 2 {
				t.Fatalf("a submit after the first POST settled: %d requests in total, want 2", n)
			}
		})
	}
}
