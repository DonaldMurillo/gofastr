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

// The hover prefetch kept only the link's pathname: a link to
// /detail?page=2 prefetched /detail, the click missed that entry, and
// fetched again. The prefetch fetches the link's URL and serves its click.
func TestHoverPrefetchKeepsQuery(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var detail []string
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	handleRuntimeModules(t, mux)
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/hub","layouts":["l:site"]},{"path":"/detail","layouts":["l:site"],"preload":"hover"}]</script>`
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` + routes +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux.HandleFunc("/hub", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<h1 id="hub">Hub</h1><a id="to-detail" href="/detail?page=2">Page 2</a>`))
	})
	mux.HandleFunc("/detail", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		detail = append(detail, fmt.Sprintf("%s prefetch=%v", r.URL.RequestURI(), r.Header.Get("X-Gofastr-Prefetch") == "1"))
		mu.Unlock()
		body := `<h1 id="detail">page ` + r.URL.Query().Get("page") + `</h1>`
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, body)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var shown string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.WaitVisible(`#detail`, chromedp.ByID),
		chromedp.Text(`#detail`, &shown, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if shown != "page 2" || len(detail) != 1 || detail[0] != "/detail?page=2 prefetch=true" {
		t.Fatalf("shown %q, /detail requests %q; want page 2 served by one prefetch of /detail?page=2", shown, detail)
	}
}
