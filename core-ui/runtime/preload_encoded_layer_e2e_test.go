package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestPrefetchDecodesEncodedSwapKey: uihost percent-encodes a layer key
// holding non-ASCII or % in X-Gofastr-Swap (fetch reads header bytes as
// Latin-1). Both navigators decode it; the preload module stored it raw,
// so the prefetched entry's layer never matched a live slot and the
// click fetched the page again. Run for the plain navigator and the
// envelope one (an outlet in the document hands navigation to it).
func TestPrefetchDecodesEncodedSwapKey(t *testing.T) {
	for _, outlet := range []bool{false, true} {
		name := "plain"
		if outlet {
			name = "envelope"
		}
		t.Run(name, func(t *testing.T) { prefetchEncodedSwapKey(t, outlet) })
	}
}

func prefetchEncodedSwapKey(t *testing.T, outlet bool) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	hits := map[string]int{}
	const key = "l:café"
	const encoded = "l:caf%C3%A9"
	side := ""
	if outlet {
		side = `<div data-cui-outlet="` + key + `#side" id="side">SIDE</div>`
	}
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">[` +
			`{"path":"/hub","layouts":["` + key + `"]},` +
			`{"path":"/detail","layouts":["` + key + `"],"preload":"hover"}]</script>` +
			`</head><body><div data-cui-layout="cafe" data-cui-layout-key="` + key + `">` + side +
			`<main role="main" tabindex="-1" data-cui-layout-slot="` + key + `">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	serve := func(inner string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				mu.Lock()
				hits[r.URL.Path]++
				mu.Unlock()
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", "t")
				w.Header().Set("X-Gofastr-Swap", encoded)
				if outlet && r.Header.Get("X-Gofastr-Fills") == "2" {
					w.Header().Set("X-Gofastr-Envelope", "2")
					fmt.Fprint(w, `<template data-cui-fill="`+key+`">`+inner+`</template>`+
						`<template data-cui-fill="`+key+`#side">SIDE</template>`)
					return
				}
				fmt.Fprint(w, inner)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, page(inner))
		}
	}
	mux.HandleFunc("/hub", serve(`<h1 id="hub">hub</h1><a id="to-detail" href="/detail">detail</a>`))
	mux.HandleFunc("/detail", serve(`<h1 id="detail">detail</h1>`))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var stamp string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub`, chromedp.ByID),
		chromedp.Evaluate(`window.__stamp = 'alive'`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.preload)`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		// Warm the envelope module first when there is one, so the
		// prefetch asks for the same envelope shape the click takes.
		chromedp.Evaluate(`window.__gofastr._navHooks.envelope || (document.querySelector('[data-cui-outlet]') && window.__gofastr.loadModule('envelope'))`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.WaitVisible(`#detail`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__stamp || 'gone'`, &stamp),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	n := hits["/detail"]
	mu.Unlock()
	if stamp != "alive" {
		t.Fatalf("stamp %q: the click left the document", stamp)
	}
	if n != 1 {
		t.Errorf("/detail navigate requests = %d, want 1: the click did not take the prefetched entry", n)
	}
}
