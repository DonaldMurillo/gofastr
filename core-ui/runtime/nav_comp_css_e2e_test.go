package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// navCSSSite shapes the two pages of a navigation-styling test. /b renders
// a component whose sheet /a never linked, and that sheet answers after a
// delay: a phone on a slow link opening a page with a new component.
type navCSSSite struct {
	aChain   bool // /a sits in the l:site layout
	bChain   bool // /b sits in the l:site layout
	outlet   bool // the layout holds an outlet, so the envelope module navigates
	partial  bool // a navigate request gets a partial, not the whole document
	envelope bool // ...and that partial is a fills envelope
	preload  bool // /b declares hover preload
	inMain   bool // the outlet sits inside the swapped slot, so a plain partial applies
}

func (c navCSSSite) serve(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var bFetches atomic.Int32
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/css/stall-probe.css", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never answers
	})
	mux.HandleFunc("/css/slow-probe.css", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(700 * time.Millisecond)
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte(`#probe{display:block;width:7px}`))
	})
	layouts := func(chain bool) string {
		if chain {
			return `["l:site"]`
		}
		return `[]`
	}
	preload := ""
	if c.preload {
		preload = `,"preload":"hover"`
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/a","layouts":` + layouts(c.aChain) + `},` +
		`{"path":"/b","layouts":` + layouts(c.bChain) + preload + `}]</script>`
	main := func(chain bool, inner string) string {
		if c.inMain {
			inner = `<aside data-cui-outlet="side">side</aside>` + inner
		}
		if !chain {
			return `<main>` + inner + `</main>`
		}
		out := ""
		if c.outlet && !c.inMain {
			out = `<aside data-cui-outlet="side">side</aside>`
		}
		return `<div data-cui-layout="site" data-cui-layout-key="l:site">` + out +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` + inner + `</main></div>`
	}
	page := func(chain bool, inner string) string {
		return `<!doctype html><html><head><title>t</title>` +
			`<script>window.__gofastr_catalog={"slow-probe":{stylePath:"/css/slow-probe.css",version:"1"},"stall-probe":{stylePath:"/css/stall-probe.css",version:"1"}};</script>` +
			routes + `</head><body>` + main(chain, inner) +
			`<script>new MutationObserver(() => { const p = document.getElementById('probe');` +
			` if (p && window.__probeW === undefined) window.__probeW = p.getBoundingClientRect().width; })` +
			`.observe(document.body, { childList: true, subtree: true });</script>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	aInner := `<a id="go" href="/b">b</a><span id="ready">ready</span>`
	bInner := `<div data-cui-comp="slow-probe" id="probe">new page</div><a id="back" href="/a">a</a>`
	answer := func(w http.ResponseWriter, r *http.Request, chain bool, inner string) {
		w.Header().Set("Content-Type", "text/html")
		if c.partial && r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "t")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			if c.envelope {
				w.Header().Set("X-Gofastr-Envelope", "2")
				fmt.Fprint(w, `<template data-cui-fill="l:site">`+inner+`</template>`+
					`<template data-cui-fill="side">side</template>`)
				return
			}
			fmt.Fprint(w, inner)
			return
		}
		fmt.Fprint(w, page(chain, inner))
	}
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) { answer(w, r, c.aChain, aInner) })
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		bFetches.Add(1)
		answer(w, r, c.bChain, bInner)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &bFetches
}

// probeWidth reads how wide the probe was at the instant it entered the
// DOM: 7px when its sheet already applied, the whole row when it did not.
// It fails the test when the click reloaded the document instead of
// navigating in place.
func probeWidth(t *testing.T, ctx context.Context, actions ...chromedp.Action) float64 {
	t.Helper()
	var width float64
	var spa bool
	all := append([]chromedp.Action{chromedp.Evaluate(`window.__spa = true; delete window.__probeW`, nil)}, actions...)
	all = append(all,
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#probe`, chromedp.ByID),
		chromedp.Evaluate(`window.__probeW`, &width),
		chromedp.Evaluate(`window.__spa === true`, &spa),
	)
	if err := chromedp.Run(ctx, all...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !spa {
		t.Fatal("the click reloaded the document instead of navigating in place")
	}
	return width
}

// TestNavWaitsForIncomingSheet: a client navigation must not paint the new
// page before the component sheets it renders have loaded. Each page links
// only the sheets it renders, so nearly every navigation brings a cold one;
// swapping first and linking after showed the deployed docs site unstyled
// on every page change on a phone. One case per swap path, in the core
// navigator and in the envelope module.
func TestNavWaitsForIncomingSheet(t *testing.T) {
	cases := []struct {
		name string
		site navCSSSite
	}{
		{"whole document", navCSSSite{}},
		{"partial", navCSSSite{aChain: true, bChain: true, partial: true}},
		{"cross chain", navCSSSite{aChain: true}},
		{"envelope partial", navCSSSite{aChain: true, bChain: true, outlet: true, partial: true, envelope: true}},
		{"envelope whole document", navCSSSite{aChain: true, bChain: true, outlet: true}},
		{"envelope cross chain", navCSSSite{aChain: true, outlet: true}},
		{"envelope plain partial", navCSSSite{aChain: true, bChain: true, inMain: true, partial: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := tc.site.serve(t)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/a"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
				t.Fatal(err)
			}
			if w := probeWidth(t, ctx); w != 7 {
				t.Fatalf("the new page entered the DOM %vpx wide, want 7: it painted before its component sheet loaded", w)
			}
		})
	}
}

// unloadProbeSheet drops the probe's sheet and every record of it, so the
// next navigation to /b needs it cold again.
const unloadProbeSheet = `(() => { const G = window.__gofastr;
	document.querySelectorAll('link[data-cui-style="slow-probe"]').forEach((l) => l.remove());
	G._pendingLinks.delete('slow-probe'); return true; })()`

// TestCachedNavWaitsForSheet: the screen-cache replay waits like a fetch
// does. A cached screen's sheets normally loaded with its first visit;
// the test drops the sheet so the replay path itself is what is proved.
func TestCachedNavWaitsForSheet(t *testing.T) {
	cases := []struct {
		name string
		site navCSSSite
	}{
		{"core", navCSSSite{aChain: true, bChain: true, partial: true}},
		{"envelope", navCSSSite{aChain: true, bChain: true, partial: true, outlet: true, envelope: true}},
		{"envelope plain entry", navCSSSite{aChain: true, bChain: true, partial: true, inMain: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, fetches := tc.site.serve(t)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/a"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
				t.Fatal(err)
			}
			probeWidth(t, ctx)
			var ok bool
			if err := chromedp.Run(ctx,
				chromedp.Click(`#back`, chromedp.ByID),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Evaluate(unloadProbeSheet, &ok),
			); err != nil {
				t.Fatal(err)
			}
			before := fetches.Load()
			if w := probeWidth(t, ctx); w != 7 {
				t.Fatalf("the cached page entered the DOM %vpx wide, want 7", w)
			}
			if n := fetches.Load() - before; n != 0 {
				t.Fatalf("the second visit fetched /b %d times: it was not the cache replay this test is about", n)
			}
		})
	}
}

// TestPrefetchedNavWaitsForSheet: a hover-prefetched page starts its
// sheets when the prefetch lands, and the click that takes the entry
// still waits for any that are in flight.
func TestPrefetchedNavWaitsForSheet(t *testing.T) {
	for _, outlet := range []bool{false, true} {
		t.Run(fmt.Sprintf("outlet=%v", outlet), func(t *testing.T) {
			srv, fetches := navCSSSite{aChain: true, bChain: true, partial: true, preload: true, outlet: outlet, envelope: outlet}.serve(t)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/a"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Evaluate(`document.getElementById('go').dispatchEvent(new PointerEvent('pointerover', { bubbles: true })); true`, nil),
			); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(5 * time.Second); fetches.Load() == 0 && time.Now().Before(deadline); {
				time.Sleep(10 * time.Millisecond)
			}
			// The prefetch answered; give the entry a moment to land.
			var warmed bool
			if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond),
				chromedp.Evaluate(`!!document.querySelector('link[data-cui-style="slow-probe"]')`, &warmed)); err != nil {
				t.Fatal(err)
			}
			if w := probeWidth(t, ctx); w != 7 {
				t.Fatalf("the prefetched page entered the DOM %vpx wide, want 7", w)
			}
			if !warmed {
				t.Error("the prefetch landed without starting the page's component sheets")
			}
			if n := fetches.Load(); n != 1 {
				t.Fatalf("/b was fetched %d times, want 1 (the prefetch): the click did not take the prefetched entry", n)
			}
		})
	}
}

// TestNavIgnoresUnrelatedSheet: a navigation waits for the sheets its
// own markup renders and no others. A sheet still in flight for
// something else (an idle prewarm, a prefetched page the reader never
// opens) must not hold the click.
func TestNavIgnoresUnrelatedSheet(t *testing.T) {
	srv, _ := navCSSSite{aChain: true, bChain: true, partial: true}.serve(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var ms float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// /b's own sheet is already loaded; the stalled one is not /b's.
		chromedp.Evaluate(`window.__gofastr.loadComponentCSS('slow-probe'); true`, nil),
		chromedp.Poll(`!!document.querySelector('link[data-cui-style="slow-probe"]').sheet`, nil),
		chromedp.Evaluate(`window.__gofastr.loadComponentCSS('stall-probe'); window.__t0 = performance.now(); true`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#probe`, chromedp.ByID),
		chromedp.Evaluate(`performance.now() - window.__t0`, &ms),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ms > 1500 {
		t.Fatalf("the navigation took %.0fms: it waited on a sheet its page does not render", ms)
	}
}

// TestEnvelopeNavOnOlderKernel: a tab opened before a deploy keeps its
// kernel and demand-loads the new envelope module. That kernel has no
// _cssReady, and the module must still navigate (unstyled for a moment,
// as before the wait existed) rather than fail every navigation.
func TestEnvelopeNavOnOlderKernel(t *testing.T) {
	srv, _ := navCSSSite{aChain: true, bChain: true, outlet: true, partial: true, envelope: true}.serve(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/a"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	probeWidth(t, ctx, chromedp.Evaluate(`delete window.__gofastr._cssReady; true`, nil))
}
