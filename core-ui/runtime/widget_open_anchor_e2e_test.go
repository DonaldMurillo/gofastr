package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Pages for the data-fui-open anchor contract (R7, tracker round 2):
// #bell is an ANCHOR widget trigger — a real link for the no-script
// page whose scripted click must open the widget and NOT navigate.
// One click used to do both: the widgets delegator opened the popover
// and the router hijacked the same click into an SPA navigation to
// the href. The trigger sits at the RIGHT edge so an anchored
// placement is distinguishable from the left-margin clamp.
func openAnchorPage() string {
	return `<!doctype html><html><head><title>open-anchor</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/inbox"}]</script>
</head><body>
  <div style="display:flex"><span style="flex:1"></span>
    <a id="bell" href="/inbox" data-fui-open="notes" data-fui-popover-anchor="bottom">Notifications</a>
  </div>
  <main>home screen</main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`
}

func openAnchorServer(t *testing.T, spaFetches *atomic.Int32) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	// A minimal live widget catalog: one hidden popover-style widget
	// whose chrome and style the delegator will fetch on open.
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"cfg":{"name":"notes","hidden":true,"closeOnEscape":true,
			"closeOnClickOutside":true,"chromePath":"/chrome/notes","stylePath":"/style/notes.css"},
			"hidden":true}]`)
	})
	mux.HandleFunc("/chrome/notes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="fui-widget fui-pos-top-right" data-fui-widget="notes" role="dialog" aria-label="Notifications"><p id="note-body">a note</p></div>`)
	})
	mux.HandleFunc("/style/notes.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `.fui-widget { position: fixed; }
.fui-widget[data-fui-popover-side] { max-inline-size: 360px; background: #fff; }`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			spaFetches.Add(1)
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "inbox")
			fmt.Fprint(w, `<p>inbox screen</p>`)
			return
		}
		fmt.Fprint(w, openAnchorPage())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestOpenAnchorClickOpensWidgetWithoutNavigating: an anchor carrying
// data-fui-open is a widget trigger, not a navigation. The router must
// stand down (URL unchanged, no partial fetch, no gofastr:navigate)
// while the widgets delegator opens the widget — and the anchored
// popover must be measured against its APPLIED stylesheet (the style
// link load is awaited), so placement lands beside the trigger rather
// than clamped to the viewport's left margin.
func TestOpenAnchorClickOpensWidgetWithoutNavigating(t *testing.T) {
	var fetches atomic.Int32
	srv := openAnchorServer(t, &fetches)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__stamp = 'live'; window.__navs = 0;
			window.addEventListener('gofastr:navigate', () => window.__navs++);`, nil),
		chromedp.Click(`#bell`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const w = document.querySelector('[data-fui-widget="notes"]');
			const r = w.getBoundingClientRect();
			return JSON.stringify({
				url: location.pathname,
				navs: window.__navs,
				stamp: window.__stamp || '',
				open: !!w && !w.hasAttribute('hidden'),
				x: Math.round(r.x), w: Math.round(r.width), vw: innerWidth,
			});
		})()`, &state),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	t.Logf("state: %s", state)
	var got struct {
		URL   string `json:"url"`
		Navs  int    `json:"navs"`
		Stamp string `json:"stamp"`
		Open  bool   `json:"open"`
		X     int    `json:"x"`
		W     int    `json:"w"`
		VW    int    `json:"vw"`
	}
	if err := json.Unmarshal([]byte(state), &got); err != nil {
		t.Fatalf("state parse: %v (%s)", err, state)
	}
	if got.URL != "/" {
		t.Errorf("a data-fui-open click navigated to %q — the URL must not change", got.URL)
	}
	if got.Stamp != "live" {
		t.Errorf("the document reloaded (stamp wiped) — the click became a hard navigation")
	}
	if got.Navs != 0 {
		t.Errorf("gofastr:navigate fired %d time(s) — the router must stand down for widget triggers", got.Navs)
	}
	if n := fetches.Load(); n != 0 {
		t.Errorf("the router fetched the href partial %d time(s) — a widget trigger is not a navigation", n)
	}
	if !got.Open {
		t.Fatal("the widget did not open — the trigger must still work as a widget trigger")
	}
	// The anchored placement must reflect the APPLIED stylesheet: with
	// max-inline-size 360px in /style/notes.css, a popover measured
	// before the sheet applied reads full-viewport width and clamps to
	// the left margin (x == 8). Measured after, it sits under the
	// trigger — x well past the left margin, box inside the viewport.
	if got.W > got.VW-16 {
		t.Errorf("popover width %d on a %d viewport — measured before its stylesheet applied", got.W, got.VW)
	}
	if got.X <= 8 {
		t.Errorf("popover pinned to the left margin (x=%d) — the stylesheet race is back", got.X)
	}
	if got.X < 0 || got.X+got.W > got.VW {
		t.Errorf("popover box [%d,%d] escapes the %d viewport", got.X, got.X+got.W, got.VW)
	}
}
