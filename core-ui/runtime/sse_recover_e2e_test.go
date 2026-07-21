package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestSSEIdleSessionRecovery pins the idle-page rollover recovery: a
// page whose SSE stream id died under it (server restart / key rotation
// / expiry) must recover WITHOUT a navigation. An EventSource can't see
// the 401 handleSSE returns, so after repeated reconnect failures the
// module POSTs /__gofastr/session, rewrites the stream meta to the fresh
// id, and reconnects — converging a purely idle tab.
func TestSSEIdleSessionRecovery(t *testing.T) {
	var minted int32
	mux := http.NewServeMux()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	// Serve the sse module bytes at its demand-load path.
	if mod, ok := Module("sse"); ok {
		mux.HandleFunc("/__gofastr/runtime/sse.js", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write([]byte(mod))
		})
	}
	// The dead stream 401s; anything else (a re-minted id) hangs open so
	// the client counts it as connected.
	mux.HandleFunc("/__gofastr/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") == "sess-DEAD" {
			http.Error(w, "unknown session", http.StatusUnauthorized)
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
	// Re-mint endpoint: hands back a fresh, non-dead id.
	mux.HandleFunc("/__gofastr/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		n := atomic.AddInt32(&minted, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sessionId":"sess-FRESH-%d"}`, n)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-DEAD">`+
			`</head><body><span id="ready">ready</span>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var meta string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// The dead stream 401s; the module retries with 3s back-off and
		// re-mints on the 2nd failure. Give it enough for a few cycles.
		chromedp.Poll(`document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content')?.indexOf('sess-DEAD') === -1`,
			nil, chromedp.WithPollingTimeout(20*time.Second), chromedp.WithPollingInterval(250*time.Millisecond)),
		chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content')`, &meta),
	); err != nil {
		t.Fatalf("meta never recovered off the dead session: %v", err)
	}
	if atomic.LoadInt32(&minted) == 0 {
		t.Error("recovery did not POST /__gofastr/session")
	}
	if meta == "/__gofastr/sse?session=sess-DEAD" || meta == "" {
		t.Errorf("stream meta still on the dead id: %q", meta)
	}
}
