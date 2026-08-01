package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestNav_RejectsStaleResponseRace pins the SPA navigator stale-response
// guard. Two overlapping navigations A→B where A's partial fetch resolves
// AFTER B's must leave the DOM, currentPath, and URL belonging to B.
//
// Without a generation guard, loadPage() swaps <main> to A's content when
// A's slow response lands last — while the URL bar already says /b and
// currentPath is /b. The user is stranded on A's content; clicking the B
// link again no-ops because fullPath === currentPath.
func TestNav_RejectsStaleResponseRace(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	var aBegun sync.WaitGroup
	aBegun.Add(1)
	releaseA := make(chan struct{})
	var aServed, bServed int

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// /a is the SLOW response — it blocks until the test releases it, so
	// /b's fetch is guaranteed to complete first.
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		aServed++
		aBegun.Done()
		<-releaseA
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main role="main"><span id="marker">PAGE-A</span></main>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		bServed++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main role="main"><span id="marker">PAGE-B</span></main>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>race</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/a"},{"path":"/b"}]</script>
</head><body>
  <main role="main" tabindex="-1"><span id="marker">HOME</span></main>
  <a id="goA" href="/a">A</a>
  <a id="goB" href="/b">B</a>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var marker, urlBar string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Click A — kicks off the SLOW /a fetch.
		chromedp.Click(`#goA`, chromedp.ByID),
		// Wait until the /a handler is mid-flight, then click B so B's
		// fetch overlaps and resolves first.
		chromedp.ActionFunc(func(ctx context.Context) error { aBegun.Wait(); return nil }),
		chromedp.Click(`#goB`, chromedp.ByID),
		// B resolved → DOM should be PAGE-B.
		chromedp.WaitVisible(`#marker`, chromedp.ByID),
		// Now release A's delayed response and let its (stale) swap run.
		chromedp.ActionFunc(func(ctx context.Context) error { close(releaseA); return nil }),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Text(`#marker`, &marker, chromedp.ByID),
		chromedp.Location(&urlBar),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if aServed < 1 || bServed < 1 {
		t.Fatalf("test is vacuous: /a served=%d /b served=%d", aServed, bServed)
	}
	if marker != "PAGE-B" {
		t.Errorf("STALE SWAP: after A's delayed response, <main> shows %q (URL=%q) — a stale nav response corrupted the DOM; expected PAGE-B", marker, urlBar)
	}
	if !contains(urlBar, "/b") {
		t.Errorf("URL bar drifted to %q — expected /b", urlBar)
	}
}
