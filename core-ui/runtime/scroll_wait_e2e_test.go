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

// TestStalledComponentCSSDoesNotBlockHashScroll pins the bound on the
// scroll settle's stylesheet wait. loadComponentCSS chains every
// component stylesheet onto the page-lifetime _stylesReady conjunction,
// and _settleScroll awaits that conjunction before writing a hash or
// history scroll position. A <link> whose request stalls — connection
// accepted, response never arriving — fires neither onload nor onerror
// and has no network timeout of its own, so without the per-link bound
// one stalled fetch poisons the conjunction (and every later link
// chained onto it) and hash scroll stops working for the rest of the
// session, silently, across every later navigation.
//
// The stall here is the destination page's own component CSS, requested
// by the post-swap scan, so the hash write on THAT navigation is the one
// that must still land (after the bound expires, not never).
func TestStalledComponentCSSDoesNotBlockHashScroll(t *testing.T) {
	var cssHits atomic.Int32
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	// The stalled stylesheet: accept the request, never answer it. The
	// handler blocks until the request context dies (client gone), so the
	// <link> never fires load or error during the test.
	mux.HandleFunc("/css/stalled.css", func(w http.ResponseWriter, r *http.Request) {
		cssHits.Add(1)
		<-r.Context().Done()
	})
	serve := func(id string, comp bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", id)
				w.Header().Set("X-Gofastr-Swap", "l:site")
				body := `<h1 id="` + id + `">` + id + `</h1><a id="to-other" style="position:fixed;top:4px;right:4px" href="/tall-a">other</a><div style="height:4000px"></div>`
				if comp {
					body = `<span data-cui-comp="stalled">styled</span>` + body
				}
				fmt.Fprint(w, body)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			page := `<!doctype html><html><head><title>t</title>` +
				`<script type="application/json" id="gofastr-routes">` +
				`[{"path":"/tall-a","layouts":["l:site"]},{"path":"/tall-b","layouts":["l:site"]}]</script>` +
				`<script>window.__gofastr_catalog={"stalled":{stylePath:"/css/stalled.css"}};</script>` +
				`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
				`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` +
				`<h1 id="` + id + `">` + id + `</h1>` +
				`<a id="to-other" style="position:fixed;top:4px;right:4px" href="/tall-b#deep">other</a>` +
				`<div style="height:4000px"></div>` +
				`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
			fmt.Fprint(w, page)
		}
	}
	mux.HandleFunc("/tall-a", serve("screen-a", false))
	mux.HandleFunc("/tall-b", func(w http.ResponseWriter, r *http.Request) {
		// The full document carries the deep target; the partial above
		// reuses the same shape plus the stalled component marker.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script>window.__gofastr_catalog={"stalled":{stylePath:"/css/stalled.css"}};</script>`+
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">`+
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">`+
			`<span data-cui-comp="stalled">styled</span>`+
			`<h1 id="screen-b">screen-b</h1>`+
			`<div style="height:600px"></div><h2 id="deep">deep target</h2><div style="height:4000px"></div>`+
			`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var reached bool
	var y float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/tall-a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		// Navigate to the deep target. The post-swap scan requests the
		// stalled stylesheet, poisoning _stylesReady; the hash write must
		// still happen once the per-link bound expires.
		chromedp.Click(`#to-other`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Poll(`window.scrollY > 400`, &reached,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(200*time.Millisecond)),
		chromedp.Evaluate(`window.scrollY`, &y),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if cssHits.Load() == 0 {
		t.Error("the stalled stylesheet was never requested — test is vacuous")
	}
	if !reached || y <= 400 {
		t.Fatalf("hash scroll never reached the deep target (reached=%v scrollY=%v): the stalled stylesheet froze the scroll settle", reached, y)
	}
}
