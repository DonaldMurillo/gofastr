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

// serveStaticCompCSSPage serves a page in the shape the static export
// writes: one SSR <link> per component in <head>, the component's marker in
// the body, and the catalog the runtime resolves names through. marked
// decides whether the SSR link carries data-fui-style, the attribute
// loadComponentCSS dedupes on.
func serveStaticCompCSSPage(t *testing.T, marked bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var cssHits atomic.Int32
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	link := `<link rel="stylesheet" href="/css/static-probe.css?v=1">`
	if marked {
		link = `<link rel="stylesheet" href="/css/static-probe.css?v=1" data-fui-style="static-probe" id="fui-css-static-probe">`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/css/static-probe.css", func(w http.ResponseWriter, _ *http.Request) {
		cssHits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte(`body{}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+link+
			`<script>window.__gofastr_catalog={"static-probe":{stylePath:"/css/static-probe.css",version:"1"}};</script>`+
			`</head><body>`+
			`<span data-fui-comp="static-probe">styled</span>`+
			`<span id="ready">ready</span>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &cssHits
}

func countStaticProbeLinks(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var links int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// The boot scan runs after the runtime script; a second link would
		// be appended synchronously by loadComponentCSS, so one more scan
		// from here settles it either way.
		chromedp.Evaluate(`window.__gofastr && window.__gofastr.scanAndLoadCSS && window.__gofastr.scanAndLoadCSS(document.body); true`, nil),
		chromedp.Evaluate(`document.querySelectorAll('link[rel="stylesheet"][href^="/css/static-probe.css"]').length`, &links),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return links
}

// TestStaticPage_MarkedSSRLinkIsNotLoadedTwice pins the contract between the
// host's SSR component links and the runtime's dedup: an SSR <link> that
// carries data-fui-style="<name>" is the one the runtime would have written,
// so the boot scan must not append another. The static export writes one
// such link per component; without the marker every static page loaded each
// component stylesheet twice, the second copy after app.css, which reversed
// the cascade against the host's own overrides.
func TestStaticPage_MarkedSSRLinkIsNotLoadedTwice(t *testing.T) {
	srv, cssHits := serveStaticCompCSSPage(t, true)
	if links := countStaticProbeLinks(t, srv); links != 1 {
		t.Fatalf("a marked SSR link was loaded %d times, want 1: the runtime's data-fui-style dedup did not see it", links)
	}
	if hits := cssHits.Load(); hits != 1 {
		t.Fatalf("the stylesheet was fetched %d times, want 1", hits)
	}
}

// TestStaticPage_UnmarkedSSRLinkIsLoadedAgain is the other half of the same
// contract, and the bug the marker fixes: an SSR link without the marker is
// invisible to the dedup, so the runtime appends its own. It is what every
// static page did before the host marked its links, and it is why the host
// must keep marking them.
func TestStaticPage_UnmarkedSSRLinkIsLoadedAgain(t *testing.T) {
	srv, _ := serveStaticCompCSSPage(t, false)
	if links := countStaticProbeLinks(t, srv); links != 2 {
		t.Fatalf("an unmarked SSR link was loaded %d times, want 2: the runtime dedupes on data-fui-style, and this page has none", links)
	}
}
