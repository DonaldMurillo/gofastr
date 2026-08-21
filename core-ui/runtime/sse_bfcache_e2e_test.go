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

// TestSSEClosesStreamOnHardNav pins the pagehide lifecycle: when the
// user hard-navigates away from an SSE-bearing page, the module must
// close its EventSource so the navigated-away page doesn't keep one of
// the tab's ~6 per-host HTTP/1.1 connections hoarded from inside the
// back/forward cache. Without the pagehide close, Chrome retains the
// dead page's stream socket (never sends FIN) and 6 SSE-bearing
// navigations starve the tab, the server here observes exactly that:
// its stream handler never unblocks.
func TestSSEClosesStreamOnHardNav(t *testing.T) {
	var active int32
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
	// The stream hangs open until the peer disconnects; `active` is the
	// server's live view of how many stream sockets exist.
	mux.HandleFunc("/__gofastr/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		fmt.Fprint(w, ": connected\n\n")
		if fl != nil {
			fl.Flush()
		}
		atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)
		<-r.Context().Done()
	})
	// Page A carries the SSE meta; page B is a plain document, so the
	// only stream that can exist after the navigation is A's leftover.
	mux.HandleFunc("/a", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-1">`+
			`</head><body><span id="ready-a">a</span>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head></head>`+
			`<body><span id="ready-b">b</span>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#ready-a`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp (page A): %v", err)
	}
	waitStreams := func(want int32, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if atomic.LoadInt32(&active) == want {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	if !waitStreams(1, 15*time.Second) {
		t.Fatalf("page A never opened its SSE stream (active = %d)", atomic.LoadInt32(&active))
	}

	// Hard navigation: a full document load of /b, which puts page A into
	// the back/forward cache. A's stream must close promptly.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/b"),
		chromedp.WaitVisible(`#ready-b`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp (page B): %v", err)
	}
	if !waitStreams(0, 10*time.Second) {
		t.Fatalf("page A's SSE stream survived the hard navigation: server still sees %d open stream(s)",
			atomic.LoadInt32(&active))
	}
}
