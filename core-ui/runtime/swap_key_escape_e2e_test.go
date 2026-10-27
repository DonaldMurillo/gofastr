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

// uihost percent-encodes a layer key holding non-ASCII in X-Gofastr-Swap,
// since fetch reads header bytes as Latin-1. The runtime decodes it and
// swaps below the kept layer instead of missing the slot and reloading
// the whole document.
func TestSwapKeyDecodesNonASCII(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	const key = "g:/p/café/:p"
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/p/a","layouts":["l:site","` + key + `"]},` +
		`{"path":"/p/b","layouts":["l:site","` + key + `"]}]</script>`
	page := func(id string) string {
		return `<!doctype html><html><head><title>t</title>` + routes + `</head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` +
			`<div data-cui-layout="p" data-cui-layout-key="` + key + `">` +
			`<nav id="side"><a id="to-b" href="/p/b">B</a></nav>` +
			`<div tabindex="-1" data-cui-layout-slot="` + key + `"><h1 id="` + id + `">` + id + `</h1></div>` +
			`</div></main></div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	var full atomic.Int64
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	for _, id := range []string{"a", "b"} {
		mux.HandleFunc("/p/"+id, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" && r.Header.Get("X-Gofastr-From") != "" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Swap", "g:/p/caf%C3%A9/:p")
				fmt.Fprintf(w, `<h1 id="%s">%s</h1>`, id, id)
				return
			}
			full.Add(1)
			fmt.Fprint(w, page(id))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	var kept bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/p/a"),
		chromedp.WaitVisible(`#to-b`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr.navigate === 'function'`, nil),
		chromedp.Evaluate(`document.getElementById('side').__mark = 1`, nil),
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('side')?.__mark === 1`, &kept),
	); err != nil {
		t.Fatal(err)
	}
	if n := full.Load(); n != 1 || !kept {
		t.Fatalf("full document loads = %d (want 1, the first), layer kept = %v", n, kept)
	}
}
