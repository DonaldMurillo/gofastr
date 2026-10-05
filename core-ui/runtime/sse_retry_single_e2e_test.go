package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// sseFlakyServer answers the FIRST /__gofastr/sse request with a 500
// (the module's onerror fires and schedules its 3 s reconnect) and
// holds every later one open.
func sseFlakyServer(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/sse", func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		fmt.Fprint(w, ": connected\n\n")
		if fl != nil {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head>`+
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-1">`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>`+
			`</head><body><main role="main" tabindex="-1"><div data-island="live"><span id="home">Home</span></div></main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestSSERescanDuringRetryOpensOneStream: while the reconnect timer
// is pending after a failed connection, a DOM insertion runs the
// module's scanner (the kernel's MutationObserver). demand() saw no
// source and connected at once, and the timer then connected AGAIN,
// so two streams stayed open for the life of the page, every island
// frame applied twice. Exactly one stream may be live afterwards.
func TestSSERescanDuringRetryOpensOneStream(t *testing.T) {
	srv := sseFlakyServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	log := newSSENetLog()
	log.listen(ctx)

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#home`, chromedp.ByID),
		// The first connection failed: retry pending.
		chromedp.Poll(`window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.retryCount >= 1`,
			nil, chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		// Any insertion rescans; a toast or an island swap would do.
		chromedp.Evaluate(`document.body.appendChild(document.createElement('p')); true`, nil),
		// Past the 3 s retry: the timer's connect lands here if it was not cancelled.
		chromedp.Sleep(4500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	live := log.count() - log.finishedCount()
	if live != 1 {
		t.Fatalf("%d live /__gofastr/sse stream(s) after the rescan (opened %d, finished %d), want exactly 1: %v",
			live, log.count(), log.finishedCount(), log.opened)
	}
}
