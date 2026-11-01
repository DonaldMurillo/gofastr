package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The route manifest lists /orders/:id:int (scoped to a document
// script) before /orders/:slug, the order the server resolves them in.
// /orders/help fails the int constraint, so the server serves the slug
// screen; the client must look it up the same way, or it reads the
// int entry's docScripts, sees no document boundary, and keeps the
// scoped capability on an unscoped page through a partial swap.
func TestTypedSegmentPicksServerRoute(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	partials := map[string]int{}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/orders/:id:int","docScripts":["/cap.js"]},{"path":"/orders/:slug"}]</script>`
	links := `<a id="to-34" href="/orders/34">34</a> <a id="to-help" href="/orders/help">help</a>`

	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/cap.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `window.__cap = true;`)
	})
	mux.HandleFunc("/orders/", func(w http.ResponseWriter, r *http.Request) {
		seg := strings.TrimPrefix(r.URL.Path, "/orders/")
		numeric := seg != "" && strings.Trim(seg, "0123456789") == ""
		body := `<h1 id="screen-` + seg + `">` + seg + `</h1>` + links
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			mu.Lock()
			partials[r.URL.Path]++
			mu.Unlock()
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, body)
			return
		}
		cap := ""
		if numeric {
			cap = `<script src="/cap.js" data-cui-doc></script>`
		}
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
			`<main role="main" tabindex="-1">`+body+`</main>`+cap+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var sameAfter34, sameAfterHelp, capAfterHelp bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/orders/12"),
		chromedp.WaitVisible(`#screen-12`, chromedp.ByID),
		chromedp.Poll(`window.__cap === true`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__sameDoc = true`, nil),
		// Same route, same scripts: a partial swap.
		chromedp.Click(`#to-34`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-34`, chromedp.ByID),
		chromedp.Evaluate(`window.__sameDoc === true`, &sameAfter34),
		// The slug route: another document.
		chromedp.Click(`#to-help`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-help`, chromedp.ByID),
		chromedp.Evaluate(`window.__sameDoc === true`, &sameAfterHelp),
		chromedp.Evaluate(`window.__cap === true`, &capAfterHelp),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sameAfter34 || partials["/orders/34"] != 1 {
		t.Fatalf("/orders/12 → /orders/34 reloaded the document (partials %v), want a partial swap", partials)
	}
	if sameAfterHelp || capAfterHelp || partials["/orders/help"] != 0 {
		t.Errorf("/orders/34 → /orders/help kept the document (same %v, capability %v, partials %v), want a document load: the client matched :id:int against \"help\"",
			sameAfterHelp, capAfterHelp, partials)
	}
}
