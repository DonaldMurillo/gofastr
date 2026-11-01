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

// TestSupersededLoadingWaitClearsItsMarks: the envelope navigator marks
// the regions it may change (aria-busy) and, on a page with loading
// content, waits for the loading module. That wait sat BEFORE the
// try/finally, so a navigation superseded during it returned without
// its cleanup: a mark the newer navigation did not take over (the inner
// layer's slot, below the newer navigation's shallower boundary)
// stayed aria-busy for good once the newer navigation failed.
func TestSupersededLoadingWaitClearsItsMarks(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">[` +
			`{"path":"/x","layouts":["l:site","l:inner"]},{"path":"/y","layouts":["l:site","l:inner"]},` +
			`{"path":"/z","layouts":["l:site"]}]</script>` +
			`</head><body><template data-cui-loading="l:site#side"><p>loading</p></template>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goY" href="/y">Y</a><a id="goZ" href="/z">Z</a></nav>` +
			`<div data-cui-outlet="l:site#side" id="side">SIDE</div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` +
			`<div data-cui-layout="inner" data-cui-layout-key="l:inner">` +
			`<section tabindex="-1" data-cui-layout-slot="l:inner" id="inner">` + inner + `</section></div>` +
			`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		if name == "loading" {
			time.Sleep(1500 * time.Millisecond)
		}
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/z" && r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "boom")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		inner := `<p>` + strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/")) + `</p>`
		if r.Header.Get("X-Gofastr-Navigate") != "1" {
			fmt.Fprint(w, page(inner))
			return
		}
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "t")
		w.Header().Set("X-Gofastr-Swap", "l:inner")
		w.Header().Set("X-Gofastr-Envelope", "2")
		fmt.Fprint(w, `<template data-cui-fill="l:inner">`+inner+`</template><template data-cui-fill="l:site#side">SIDE</template>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var busy []string
	var loaded bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/x"),
		chromedp.WaitVisible(`#goY`, chromedp.ByID),
		// The envelope module first, alone: the loading module it starts
		// at evaluation then arrives 1.5s later.
		chromedp.Evaluate(`window.__gofastr.loadModule('envelope')`, nil),
		chromedp.Poll(`!!window.__gofastr._navHooks.envelope`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#goY`, chromedp.ByID), // marks #inner, waits on the loading module
		chromedp.Poll(`document.getElementById('inner').getAttribute('aria-busy') === 'true'`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#goZ`, chromedp.ByID), // supersedes; its answer fails
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.Evaluate(`!!window.__gofastr._navHooks.loading`, &loaded),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-busy="true"]')].map((e) => e.id || e.tagName)`, &busy),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !loaded {
		t.Fatal("the loading module never loaded: the scenario is vacuous")
	}
	if len(busy) != 0 {
		t.Errorf("regions still aria-busy after both navigations settled: %v", busy)
	}
}
