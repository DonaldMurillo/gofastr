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

// An RPC control inside SSR-inlined widget chrome works before the
// widget catalog mounts that chrome. The document click bridge skipped
// every click inside [data-cui-widget], trusting the widget's own
// listener, which exists only once mountWidget ran: until the catalog
// answered (or forever, if it never lists the widget) the click was
// swallowed. Once the widget is mounted its listener owns the click
// and the bridge stands down, so a click still sends one request. The
// control is an input: a <button> with no value is busy-guarded, which
// would hide a second dispatch of the same click.
func TestSSRWidgetRPCWorksBeforeMount(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	release := make(chan struct{})
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"hidden":false,"cfg":{"name":"panel","stylePath":"/panel.css"}}]`)
	})
	mux.HandleFunc("/panel.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
	})
	mux.HandleFunc("/act", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>ssr widget</title></head><body><main>home</main>
<div class="cui-widget" data-cui-widget="panel"><input type="button" id="act" data-cui-rpc="/act" value="Act"></div>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#act`, chromedp.ByID),
		chromedp.Click(`#act`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("a click on unmounted SSR widget chrome sent %d requests, want 1", n)
	}
	close(release)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!(window.__gofastr._widgets.panel && window.__gofastr._widgets.panel.root)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`#act`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("after mount: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("after the widget mounted, one more click left %d requests in total, want 2 (one per click)", n)
	}
	// A dismissed hydrated root keeps its node (hidden) but loses its
	// listener: shown again by anything but a mount, the bridge serves it.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.closeWidget('panel'); document.querySelector('[data-cui-widget="panel"]').removeAttribute('hidden'); true`, nil),
		chromedp.Click(`#act`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("after dismiss: %v", err)
	}
	if n := hits.Load(); n != 3 {
		t.Fatalf("a click on a dismissed root left %d requests in total, want 3", n)
	}
}
