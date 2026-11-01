package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The server percent-decodes a segment before it tests a typed
// constraint (app.RoutePath), so /orders/%33%34 is the :id:int route.
// The client's lookup must decode the same way, or it reads the slug
// route, sees a document boundary that is not there, and reloads. A
// segment that does not decode (%E0%A4) is the slug route on both
// sides, and the lookup must not throw on it: /things has no document
// scripts, so a lookup that throws turns its partial swap into a load.
func TestRouteLookupDecodesSegment(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	partials := map[string]int{}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/orders/:id:int","docScripts":["/cap.js"]},{"path":"/orders/:slug"},` +
		`{"path":"/things/:id:int"},{"path":"/things/:slug"}]</script>`
	links := `<a id="to-enc" href="/orders/%33%34">34</a> <a id="to-bad" href="/things/x%E0%A4">bad</a>`

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
	page := func(w http.ResponseWriter, r *http.Request) {
		ep := r.URL.EscapedPath()
		raw := ep[strings.LastIndex(ep, "/")+1:]
		seg := raw
		if d, err := url.PathUnescape(raw); err == nil {
			seg = d
		}
		numeric := seg != "" && strings.Trim(seg, "0123456789") == ""
		id := "slug"
		if numeric {
			id = seg
		}
		body := `<h1 id="screen-` + id + `">` + id + `</h1>` + links
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			mu.Lock()
			partials[raw]++
			mu.Unlock()
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, body)
			return
		}
		cap := ""
		if numeric && strings.HasPrefix(r.URL.Path, "/orders/") {
			cap = `<script src="/cap.js" data-cui-doc></script>`
		}
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
			`<main role="main" tabindex="-1">`+body+`</main>`+cap+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	}
	mux.HandleFunc("/orders/", page)
	mux.HandleFunc("/things/", page)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var sameAfterEnc, sameAfterBad bool
	var errs string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/orders/12"),
		chromedp.WaitVisible(`#screen-12`, chromedp.ByID),
		chromedp.Poll(`window.__cap === true`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__sameDoc = true; window.__errs = []; window.addEventListener('error', function (e) { window.__errs.push(String(e.message)); }); true`, nil),
		// %33%34 is 34: the int route, same scripts, a partial swap.
		chromedp.Click(`#to-enc`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-34`, chromedp.ByID),
		chromedp.Evaluate(`window.__sameDoc === true`, &sameAfterEnc),
		chromedp.Evaluate(`(window.__errs || []).join('; ')`, &errs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	encPartials := partials["%33%34"]
	mu.Unlock()
	if !sameAfterEnc || encPartials != 1 {
		t.Fatalf("/orders/12 → /orders/%%33%%34 reloaded the document (partials %d), want a partial swap: the client tested the raw segment against :id:int", encPartials)
	}
	if errs != "" {
		t.Fatalf("navigation threw: %s", errs)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/things/1"),
		chromedp.WaitVisible(`#screen-1`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadModule)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__sameDoc = true`, nil),
		// x%E0%A4 does not decode: the slug route, the same document.
		chromedp.Click(`#to-bad`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-slug`, chromedp.ByID),
		chromedp.Evaluate(`window.__sameDoc === true`, &sameAfterBad),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sameAfterBad || partials["x%E0%A4"] != 1 {
		t.Errorf("/things/1 → /things/x%%E0%%A4 reloaded the document (partials %v), want a partial swap: the lookup threw on a malformed escape", partials)
	}
}
