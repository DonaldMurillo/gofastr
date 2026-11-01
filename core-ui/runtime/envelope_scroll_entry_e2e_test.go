package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestEnvelopeBackRestoresEarlierVisitScroll: the envelope navigator's
// scroll anchors are recorded per PATH and win over core's per-entry
// pixels. /list@2000 → /item → /list (a new entry) @500 → Back → Back
// reaches the FIRST /list entry, whose own position is 2000, but the
// anchor recorded by the later visit put it at 500.
func TestEnvelopeBackRestoresEarlierVisitScroll(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var rows strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&rows, `<p id="r%d" style="height:100px;margin:0">row %d</p>`, i, i)
	}
	link := `<a id="nav" style="position:fixed;top:4px;right:4px" href="%s">go</a>`
	list := `<h1 id="list">list</h1>` + fmt.Sprintf(link, "/item") + rows.String()
	item := `<h1 id="item">item</h1>` + fmt.Sprintf(link, "/list") + `<div style="height:5000px"></div>`
	page := func(inner string) string {
		return `<!doctype html><html lang="en"><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/list","layouts":["l:site"]},{"path":"/item","layouts":["l:site"]}]</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<div data-cui-outlet="l:site#side" id="side">SIDE</div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
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
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Swap", "l:site")
				w.Header().Set("X-Gofastr-Envelope", "2")
				fmt.Fprint(w, `<template data-cui-fill="l:site">`+inner+`</template>`+
					`<template data-cui-fill="l:site#side">SIDE</template>`)
				return
			}
			fmt.Fprint(w, page(inner))
		}
	}
	mux.HandleFunc("/list", serve(list))
	mux.HandleFunc("/item", serve(item))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800), chromedptest.Timeout(90*time.Second))
	settle := chromedp.Sleep(400 * time.Millisecond)
	var second, first float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#list`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 2000)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Click(`#nav`, chromedp.ByID), // → /item
		chromedp.WaitVisible(`#item`, chromedp.ByID),
		settle,
		chromedp.Click(`#nav`, chromedp.ByID), // → /list, a new entry
		chromedp.WaitVisible(`#list`, chromedp.ByID),
		settle,
		chromedp.Evaluate(`window.scrollTo(0, 500)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil), // → /item
		chromedp.WaitVisible(`#item`, chromedp.ByID),
		settle,
		chromedp.Evaluate(`history.forward()`, nil), // → the second /list entry
		chromedp.WaitVisible(`#list`, chromedp.ByID),
		settle,
		chromedp.Evaluate(`window.scrollY`, &second),
		chromedp.Evaluate(`history.back()`, nil), // → /item
		chromedp.WaitVisible(`#item`, chromedp.ByID),
		settle,
		chromedp.Evaluate(`history.back()`, nil), // → the first /list entry
		chromedp.WaitVisible(`#list`, chromedp.ByID),
		settle,
		chromedp.Evaluate(`window.scrollY`, &first),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if second < 450 || second > 550 {
		t.Errorf("second /list entry scrollY = %v, want ~500", second)
	}
	if first < 1950 || first > 2050 {
		t.Errorf("first /list entry scrollY = %v, want ~2000: the later visit's anchor overrode this entry's position", first)
	}
}
