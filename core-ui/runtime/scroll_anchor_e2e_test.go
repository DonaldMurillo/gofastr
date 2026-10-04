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

// scrollAnchorSite serves two same-chain pages. The destination's own
// component stylesheet arrives LATE (delayMs) and, when applied, grows
// content ABOVE the viewport by padPx — the classic scroll-anchoring
// trigger: the browser adjusts scrollY to keep the visible content
// stable even though no user input happened.
func scrollAnchorSite(t *testing.T, delayMs int, padPx int) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/css/cold.css", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprintf(w, `[data-cui-comp="cold"] { display: block; block-size: %dpx; }`, padPx)
	})
	page := func(id, href string, comp bool) string {
		compSpan := ""
		if comp {
			compSpan = `<span data-cui-comp="cold">cold</span>`
		}
		return `<!doctype html><html><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">` +
			`[{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}]</script>` +
			`<script>window.__gofastr_catalog={"cold":{stylePath:"/css/cold.css"}};</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` +
			`<h1 id="` + id + `">` + id + `</h1>` +
			`<a id="to-b" style="position:fixed;top:4px;right:4px" href="` + href + `">to b</a>` +
			compSpan +
			`<div style="height:600px"></div><h2 id="deep">deep target</h2>` +
			`<div style="height:4000px"></div>` +
			`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "a")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, `<h1 id="screen-a">a</h1><a id="to-b" style="position:fixed;top:4px;right:4px" href="/b#deep">to b</a><div style="height:4000px"></div>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page("screen-a", "/b#deep", false))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "b")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, `<h1 id="screen-b">b</h1><span data-cui-comp="cold">cold</span><div style="height:600px"></div><h2 id="deep">deep target</h2><div style="height:4000px"></div>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page("screen-b", "/a", true))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestScrollAnchoringDriftDoesNotCancelHashScroll pins the scroll
// settle's cancel test. _settleScroll captures its cancel baseline
// before awaiting the component-stylesheet conjunction; the wait exists
// precisely because cold CSS lands after the swap, and that CSS can
// grow content above the viewport, at which point the browser's scroll
// anchoring moves scrollY with no user input (observed in this repo's
// headless Chrome: 1200 → 2682 on a 1482px growth). A pixel-identity
// cancel test read that drift as a user scroll and dropped the first
// write entirely — the hash target was never reached. Only real input
// (wheel/touchmove/keydown/pointerdown) may cancel the write.
func TestScrollAnchoringDriftDoesNotCancelHashScroll(t *testing.T) {
	srv := scrollAnchorSite(t, 1200, 1500)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800), chromedptest.Timeout(90*time.Second))
	var yDuringWait, deepEarly, yFinal, deepFinal float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 1200)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		// The navigation whose destination grows above the viewport when
		// its cold stylesheet finally arrives (~1.2s after the swap).
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		// Still inside the stylesheet wait: the position the previous
		// page was left at, with the destination's pre-CSS layout.
		chromedp.Evaluate(`window.scrollY`, &yDuringWait),
		chromedp.Evaluate(`document.getElementById('deep').offsetTop`, &deepEarly),
		// Let the cold CSS land (well inside the 3s bound), the anchor
		// drift happen, and the settle write follow it.
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &yFinal),
		chromedp.Evaluate(`document.getElementById('deep').offsetTop`, &deepFinal),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if yDuringWait != 1200 {
		t.Fatalf("scrollY during the stylesheet wait = %v, want 1200 — the wait did not hold the write back, so the test does not exercise the settle window", yDuringWait)
	}
	// The cold stylesheet must actually have moved the target by the end
	// (pre-CSS offset ~718 → ~2200), or the anchoring scenario is vacuous.
	if deepEarly > 800 || deepFinal < 2000 {
		t.Fatalf("deep target offset early=%v final=%v, want ≈718 → ≈2200 — the late stylesheet never grew the content above the viewport", deepEarly, deepFinal)
	}
	if diff := yFinal - deepFinal; diff < -25 || diff > 25 {
		t.Errorf("final scrollY = %v, want ≈%v (the hash target): scroll-anchoring drift during the stylesheet wait cancelled the hash write", yFinal, deepFinal)
	}
}
