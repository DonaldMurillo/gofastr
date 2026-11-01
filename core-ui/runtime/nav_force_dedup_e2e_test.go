package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestForcedNavigateNotDedupedByCacheReplay: a click on /b replays the
// cached copy and waits on its component sheets; navigate('/b', {force:
// true}) issued meanwhile (a mutation just made that copy stale) met the
// in-flight dedup, which saw the live replay for the same path and
// dropped the forced fetch. The stale copy then landed. A forced
// navigation must never join a cached one.
func TestForcedNavigateNotDedupedByCacheReplay(t *testing.T) {
	for _, outlet := range []bool{false, true} {
		name := "plain"
		if outlet {
			name = "envelope"
		}
		t.Run(name, func(t *testing.T) { forcedNavigateNotDeduped(t, outlet) })
	}
}

func forcedNavigateNotDeduped(t *testing.T, outlet bool) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	hits := map[string]int{}
	side := ""
	if outlet {
		side = `<div data-cui-outlet="l:site#side" id="side">SIDE</div>`
	}
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">[` +
			`{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}]</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a><a id="goB" href="/b">B</a></nav>` + side +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		n := hits[r.URL.Path]
		mu.Unlock()
		inner := fmt.Sprintf(`<p>MAIN-%s-%d</p>`, strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/")), n)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") != "1" {
			fmt.Fprint(w, page(inner))
			return
		}
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "t")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		if outlet && r.Header.Get("X-Gofastr-Fills") == "2" {
			w.Header().Set("X-Gofastr-Envelope", "2")
			fmt.Fprint(w, `<template data-cui-fill="l:site">`+inner+`</template><template data-cui-fill="l:site#side">SIDE</template>`)
			return
		}
		fmt.Fprint(w, inner)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	mainHas := func(s string) chromedp.Action {
		return chromedp.Poll(`document.getElementById('main').textContent.includes('`+s+`')`, new(bool), chromedp.WithPollingTimeout(10*time.Second))
	}
	var main string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#goB`, chromedp.ByID),
		chromedp.Click(`#goB`, chromedp.ByID),
		mainHas("MAIN-B-1"),
		chromedp.Click(`#goA`, chromedp.ByID),
		mainHas("MAIN-A"),
		chromedp.Sleep(200*time.Millisecond),
		// Every sheet wait from here on holds until released.
		chromedp.Evaluate(`(() => { const G = window.__gofastr, orig = G._cssReady;
			G.__held = new Promise((r) => { G.__release = r; });
			G.__holding = true;
			G._cssReady = (h) => G.__holding ? G.__held : orig.call(G, h); })()`, nil),
		chromedp.Click(`#goB`, chromedp.ByID), // the cached replay of MAIN-B-1, held
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.__holding = false; window.__gofastr.navigate('/b', {force: true})`, nil),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.__release()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('main').textContent`, &main),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(main, "MAIN-B-2") {
		t.Errorf("main = %q, want the forced fetch's MAIN-B-2: the forced navigation joined the cached replay", main)
	}
}
