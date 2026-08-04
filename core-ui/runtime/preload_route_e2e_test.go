package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// preloadSite: /hub declares hover-preload for /detail and an eager route
// /eager; /pop is an intercepting route from /hub (must never prefetch).
type preloadReq struct {
	Path     string
	Prefetch bool
}

func newPreloadSite(t *testing.T) (*httptest.Server, func() []preloadReq) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var reqs []preloadReq
	record := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		reqs = append(reqs, preloadReq{Path: r.URL.Path, Prefetch: r.Header.Get("X-Gofastr-Prefetch") == "1"})
	}
	requests := func() []preloadReq {
		mu.Lock()
		defer mu.Unlock()
		return append([]preloadReq(nil), reqs...)
	}

	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/hub","layouts":["l:site"]},` +
		`{"path":"/detail","layouts":["l:site"],"preload":"hover"},` +
		`{"path":"/eager","layouts":["l:site"],"preload":"eager"},` +
		`{"path":"/pop","layouts":["l:site"],"preload":"hover","intercept":{"from":"/hub","as":"drawer"}}` +
		`]</script>`
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` + routes +
			`</head><body><div data-fui-layout="site" data-fui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:site">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	hubInner := `<h1 id="hub-screen">Hub</h1>` +
		`<a id="to-detail" href="/detail">Detail</a>` +
		`<a id="to-pop" href="/pop">Pop</a>`

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/__gofastr/runtime/"):]
		name = name[:len(name)-len(".js")]
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})
	partial := func(w http.ResponseWriter, title, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", title)
		w.Header().Set("X-Gofastr-Swap", "l:site")
		fmt.Fprint(w, body)
	}
	mux.HandleFunc("/hub", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "Hub", hubInner)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(hubInner))
	})
	mux.HandleFunc("/detail", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "Detail", `<h1 id="detail-screen">Detail</h1>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<h1 id="detail-screen">Detail</h1>`))
	})
	mux.HandleFunc("/eager", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "Eager", `<h1 id="eager-screen">Eager</h1>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<h1 id="eager-screen">Eager</h1>`))
	})
	mux.HandleFunc("/pop", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		partial(w, "Pop", `<div id="pop-overlay">Pop</div>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, requests
}

func countReqs(reqs []preloadReq, path string) (total, prefetch int) {
	for _, r := range reqs {
		if r.Path == path {
			total++
			if r.Prefetch {
				prefetch++
			}
		}
	}
	return
}

// Hover on a preload:"hover" link fires exactly one prefetch (marked
// X-Gofastr-Prefetch); the click then paints from the side cache with no
// second request. Eager routes prefetch at idle with no interaction.
// Intercepting routes are never prefetched from their origin.
func TestHoverPrefetchServesClick(t *testing.T) {
	srv, requests := newPreloadSite(t)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond), // module load + eager idle pass
		// Hover the detail link, give the prefetch a beat, then click.
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.WaitVisible(`#detail-screen`, chromedp.ByID),
		// Hover the intercepting route's link too — must NOT prefetch.
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	reqs := requests()
	total, pf := countReqs(reqs, "/detail")
	if pf != 1 {
		t.Errorf("/detail prefetch requests = %d, want 1", pf)
	}
	if total != 1 {
		t.Errorf("/detail total requests = %d, want 1 (click must reuse the prefetched entry)", total)
	}
	if et, _ := countReqs(reqs, "/eager"); et == 0 {
		t.Error("eager route was never prefetched at idle")
	} else if _, ep := countReqs(reqs, "/eager"); ep != et {
		t.Error("eager requests must all be prefetch-marked")
	}
}

func TestInterceptedRouteNeverPrefetched(t *testing.T) {
	srv, requests := newPreloadSite(t)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('to-pop').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if _, pf := countReqs(requests(), "/pop"); pf != 0 {
		t.Errorf("intercepting route was prefetched %d times from its origin; overlay HTML must never enter a cache", pf)
	}
}

// TTL expiry: a prefetched entry older than the TTL is discarded and the
// click refetches. The TTL is shrunk via the test hook rather than
// waiting 30s.
func TestPrefetchTTLExpiryRefetches(t *testing.T) {
	srv, requests := newPreloadSite(t)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`__gofastr._preloadTTLms = 50`, nil),
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(500*time.Millisecond), // > TTL
		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.WaitVisible(`#detail-screen`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	total, pf := countReqs(requests(), "/detail")
	if pf != 1 || total != 2 {
		t.Errorf("/detail requests = %d (prefetch %d), want 2 total / 1 prefetch — expired entry must refetch", total, pf)
	}
}

// invalidate() must evict prefetched entries with the same selector
// semantics as the screen cache.
func TestInvalidateEvictsPrefetched(t *testing.T) {
	srv, requests := newPreloadSite(t)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`__gofastr.invalidate('/detail')`, nil),
		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.WaitVisible(`#detail-screen`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	total, pf := countReqs(requests(), "/detail")
	if pf != 1 || total != 2 {
		t.Errorf("/detail requests = %d (prefetch %d), want 2 total — invalidate must evict the prefetched entry", total, pf)
	}
}
