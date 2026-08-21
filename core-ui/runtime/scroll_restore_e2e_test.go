package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// scrollSite serves two tall same-chain pages so back/forward/reload can
// prove per-history-entry scroll restoration.
func scrollSite(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// The nav link is position:fixed so chromedp's click never scrolls it
	// into view, a pre-click scroll would (correctly!) be captured as
	// the entry's final position and defeat what the test measures.
	link := func(other string) string {
		return `<a id="to-other" style="position:fixed;top:4px;right:4px" href="` + other + `">other</a>`
	}
	page := func(id, other string) string {
		return `<!doctype html><html><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">` +
			`[{"path":"/tall-a","layouts":["l:site"]},{"path":"/tall-b","layouts":["l:site"]}]</script>` +
			`</head><body><div data-fui-layout="site" data-fui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:site">` +
			`<h1 id="` + id + `">` + id + `</h1>` +
			link(other) +
			`<div style="height:4000px"></div>` +
			`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	serve := func(id, other string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", id)
				w.Header().Set("X-Gofastr-Swap", "l:site")
				fmt.Fprint(w, `<h1 id="`+id+`">`+id+`</h1><a id="to-other" style="position:fixed;top:4px;right:4px" href="`+other+`">other</a><div style="height:4000px"></div>`)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page(id, other))
		}
	}
	mux.HandleFunc("/tall-a", serve("screen-a", "/tall-b"))
	mux.HandleFunc("/tall-b", serve("screen-b", "/tall-a"))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBackRestoresScrollOffset(t *testing.T) {
	srv := scrollSite(t)
	ctx := newSeedBrowserCtx(t)

	var backY, forwardTopY float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/tall-a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 1200)`, nil),
		chromedp.Sleep(250*time.Millisecond), // capture + persist throttle
		chromedp.Click(`#to-other`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond), // double-rAF settle
		chromedp.Evaluate(`window.scrollY`, &backY),
		chromedp.Evaluate(`history.forward()`, nil),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &forwardTopY),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if backY < 1000 || backY > 1400 {
		t.Errorf("back scrollY = %v, want ≈1200 (per-entry restore)", backY)
	}
	// B was left at the top; forward must restore that, not A's offset.
	if forwardTopY > 100 {
		t.Errorf("forward scrollY = %v, want ≈0 (B's own position)", forwardTopY)
	}
}

func TestForwardRestoresScrollOffset(t *testing.T) {
	srv := scrollSite(t)
	ctx := newSeedBrowserCtx(t)

	// Trace the observable internals at every step: CI Chrome fails this
	// flow in ways local Chrome doesn't, and the failure message must say
	// which link broke (entry ids, stored positions, popstate firing).
	snap := `JSON.stringify({st: history.state, ss: sessionStorage.getItem('gofastr:scroll'), y: scrollY, path: location.pathname})`
	var onB, afterBack, afterForward string
	var y float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/tall-a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Click(`#to-other`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 800)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(snap, &onB),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(snap, &afterBack),
		chromedp.Evaluate(`history.forward()`, nil),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(snap, &afterForward),
		chromedp.Evaluate(`window.scrollY`, &y),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if y < 600 || y > 1000 {
		t.Errorf("forward scrollY = %v, want ≈800 — Forward restore is the previously-untested half\n  on B:          %s\n  after back:    %s\n  after forward: %s",
			y, onB, afterBack, afterForward)
	}
}

// Rapid back-then-forward: the back navigation's delayed settle write
// (scroll to top) must not land after, and clobber, the forward
// navigation's restored position. Regression for the _scrollSeq guard.
func TestRapidBackForwardKeepsForwardScroll(t *testing.T) {
	srv := scrollSite(t)
	ctx := newSeedBrowserCtx(t)

	snap := `JSON.stringify({st: history.state, ss: sessionStorage.getItem('gofastr:scroll'), y: scrollY, path: location.pathname})`
	var after string
	var y float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/tall-a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Click(`#to-other`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 800)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		// No settling time between the two moves, the back nav's
		// double-rAF settle pass is still queued when forward lands.
		chromedp.Evaluate(`history.back(); setTimeout(() => history.forward(), 30)`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(snap, &after),
		chromedp.Evaluate(`window.scrollY`, &y),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if y < 600 || y > 1000 {
		t.Errorf("scrollY after rapid back+forward = %v, want ≈800 — a superseded nav's settle write clobbered the restore\n  final: %s", y, after)
	}
}

func TestReloadRestoresScrollViaSessionStorage(t *testing.T) {
	srv := scrollSite(t)
	ctx := newSeedBrowserCtx(t)

	var y float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/tall-a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 900)`, nil),
		chromedp.Sleep(300*time.Millisecond), // persist throttle (150ms)
		chromedp.Reload(),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &y),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if y < 700 || y > 1100 {
		t.Errorf("reload scrollY = %v, want ≈900 — manual scrollRestoration needs the sessionStorage path", y)
	}
}
