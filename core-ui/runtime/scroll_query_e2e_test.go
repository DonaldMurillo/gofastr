package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// querySite serves one tall list page whose fixed links change only its
// query (a sort) or go to another path. With envelope set the layout
// holds an outlet, so the envelope module's navigator runs instead of
// core's.
func querySite(t *testing.T, envelope bool) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	body := func(r *http.Request) string {
		return `<h1 id="list">list ` + r.URL.RawQuery + `</h1>` +
			`<a id="sort" style="position:fixed;top:4px;right:4px" href="/list?sort=amount">sort</a>` +
			`<a id="away" style="position:fixed;top:40px;right:4px" href="/other">away</a>` +
			`<div style="height:4000px"></div>`
	}
	outlet := ""
	if envelope {
		outlet = `<div data-cui-outlet="l:site#side">SIDE</div>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	serve := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "t")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			if envelope {
				w.Header().Set("X-Gofastr-Envelope", "2")
				fmt.Fprint(w, `<template data-cui-fill="l:site">`+body(r)+`</template>`)
				return
			}
			fmt.Fprint(w, body(r))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">`+
			`[{"path":"/list","layouts":["l:site"]},{"path":"/other","layouts":["l:site"]}]</script>`+
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">`+outlet+
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">`+body(r)+
			`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
	}
	mux.HandleFunc("/list", serve)
	mux.HandleFunc("/other", serve)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A sort link changes only the query: the page swaps and the reader stays
// where they were. A link to another path still starts at the top. Core
// and the envelope module each run their own navigation tail.
func TestQueryNavKeepsScroll(t *testing.T) {
	t.Run("core", func(t *testing.T) { queryNavKeepsScroll(t, false) })
	t.Run("envelope", func(t *testing.T) { queryNavKeepsScroll(t, true) })
}

func queryNavKeepsScroll(t *testing.T, envelope bool) {
	srv := querySite(t, envelope)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var heading string
	var loaded bool
	var sortY, awayY float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#list`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 1200)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Click(`#sort`, chromedp.ByID),
		chromedp.WaitVisible(`#list`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Text(`#list`, &heading, chromedp.ByID),
		chromedp.Evaluate(`!!window.__gofastr._navHooks.envelope`, &loaded),
		chromedp.Evaluate(`window.scrollY`, &sortY),
		chromedp.Click(`#away`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &awayY),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if loaded != envelope {
		t.Fatalf("envelope module loaded = %v, want %v", loaded, envelope)
	}
	if heading != "list sort=amount" {
		t.Fatalf("the sort link did not swap the page: heading %q", heading)
	}
	if sortY < 1000 || sortY > 1400 {
		t.Errorf("after the sort scrollY = %v, want ≈1200", sortY)
	}
	if awayY > 100 {
		t.Errorf("a new path kept scrollY = %v, want the top", awayY)
	}
}
