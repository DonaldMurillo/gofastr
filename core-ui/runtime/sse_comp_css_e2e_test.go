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

// TestSSE_IslandSwapLoadsComponentCSS pins that an SSE island update
// whose HTML introduces a NEW [data-fui-comp] gets its component CSS
// loaded. nav / signals / poll / widgets / infinitescroll all call
// scanAndLoadCSS after their innerHTML swap, sse.js was the only swap
// path that did not, so a server-pushed island bringing in a styled
// component rendered unstyled.
func TestSSE_IslandSwapLoadsComponentCSS(t *testing.T) {
	var cssHits atomic.Int32
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	if mod, ok := Module("sse"); ok {
		mux.HandleFunc("/__gofastr/runtime/sse.js", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte(mod))
		})
	}
	mux.HandleFunc("/__gofastr/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		fmt.Fprint(w, ": connected\n\n")
		if fl != nil {
			fl.Flush()
		}
		// Push an island frame whose HTML introduces a brand-new
		// [data-fui-comp] the page did not carry at boot.
		fmt.Fprint(w, "event: island\ndata: {\"island\":\"live\",\"html\":\"<span data-fui-comp=\\\"sse-probe\\\">arrived</span>\"}\n\n")
		if fl != nil {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("/css/sse-probe.css", func(w http.ResponseWriter, _ *http.Request) {
		cssHits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte(`body{}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+
			`<meta name="gofastr-sse" content="/__gofastr/sse">`+
			`<script>window.__gofastr_catalog={"sse-probe":{stylePath:"/css/sse-probe.css"}};</script>`+
			`</head><body>`+
			`<div id="live" data-island="live"><span id="before">before</span></div>`+
			`<span id="ready">ready</span>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	var hasLink, swapped bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Wait for the island swap to land (the new content appears).
		chromedp.Poll(`document.querySelector('#live') && document.querySelector('#live').textContent.indexOf('arrived') >= 0`,
			nil, chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(150*time.Millisecond)),
		chromedp.Evaluate(`!!document.querySelector('link[data-fui-style="sse-probe"]')`, &hasLink),
		chromedp.Evaluate(`document.querySelector('#live').textContent.indexOf('arrived') >= 0`, &swapped),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !swapped {
		t.Fatal("island swap never landed — test is vacuous")
	}
	if !hasLink {
		t.Error("SSE island swap did not load component CSS: no <link data-fui-style=\"sse-probe\"> after a swap that introduced [data-fui-comp=\"sse-probe\"] — scanAndLoadCSS is missing from the SSE swap path")
	}
	if cssHits.Load() == 0 {
		t.Error("SSE island swap did not fetch /css/sse-probe.css — component CSS never requested")
	}
}
