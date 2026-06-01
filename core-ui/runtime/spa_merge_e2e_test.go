package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestSPAMerge_GlobalSurvivesNavPageScopedReseeds proves the SPA-nav
// merge rule: navigating from / to /b must NOT clobber an app-global the
// user already mutated (cart-style counter), while the destination
// page's page-scoped slice IS freshly seeded.
func TestSPAMerge_GlobalSurvivesNavPageScopedReseeds(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// /b — partial (SPA) returns just the content + the scope-split seed
	// island. Global g.count seeded 0; page-scoped b.local seeded "B".
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"b.local":"B"},"g":{"g.count":0}}</script>`+
			`<h1 id="pageB">Page B</h1>`+
			`<span id="bcount" data-fui-signal="g.count">0</span>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/b"}]</script>
  <script type="application/json" id="gofastr-signals">{"g.count":0,"a.local":"A"}</script>
</head><body>
  <main role="main" tabindex="-1">
    <span id="count" data-fui-signal="g.count">0</span>
    <button id="inc" data-fui-signal-inc="g.count">+</button>
    <a id="tob" href="/b">to B</a>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var countAfterInc, gAfterNav, bLocal string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Mutate the global twice → 2.
		chromedp.Click(`#inc`, chromedp.ByID),
		chromedp.Click(`#inc`, chromedp.ByID),
		chromedp.Text(`#count`, &countAfterInc, chromedp.ByID),
		// SPA-navigate to /b.
		chromedp.Click(`#tob`, chromedp.ByID),
		chromedp.WaitVisible(`#pageB`, chromedp.ByID),
		// Global must survive (still 2), NOT reset to the partial's 0.
		chromedp.Evaluate(`String(window.__gofastr.getSignal('g.count'))`, &gAfterNav),
		// Page-scoped slice from the new page must be seeded.
		chromedp.Evaluate(`String(window.__gofastr.getSignal('b.local'))`, &bLocal),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if countAfterInc != "2" {
		t.Fatalf("pre-nav global = %q, want 2", countAfterInc)
	}
	if gAfterNav != "2" {
		t.Errorf("global clobbered by SPA-nav seed: g.count = %q, want preserved 2", gAfterNav)
	}
	if bLocal != "B" {
		t.Errorf("destination page-scoped slice not seeded: b.local = %q, want B", bLocal)
	}
}
