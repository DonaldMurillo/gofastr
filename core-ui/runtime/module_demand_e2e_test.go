package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The module-demand rig (the Opt-in spec's per-module rows): one page
// per trigger, each carrying ONLY its own marker, plus a shared shell
// so a click navigation always has somewhere to go. The server logs
// module requests and the headers of every navigation request, and
// can gate a page answer to create in-flight time.
type demandSite struct {
	srv *httptest.Server

	mu         sync.Mutex
	moduleReqs map[string]int
	navHdrs    []map[string]string // one map per X-Gofastr-Navigate request
	pageHold   chan struct{}       // non-nil: /a navigate answers wait for its close
}

// holdPages arms the in-flight window: every gated page answer waits
// until releasePages, so a test can observe the busy marks and the
// loading scheduler while a navigation is genuinely in flight.
func (s *demandSite) holdPages() {
	s.mu.Lock()
	s.pageHold = make(chan struct{})
	s.mu.Unlock()
}

func (s *demandSite) releasePages() {
	s.mu.Lock()
	if s.pageHold != nil {
		close(s.pageHold)
		s.pageHold = nil
	}
	s.mu.Unlock()
}

func (s *demandSite) bumpModule(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moduleReqs[name]++
}

func (s *demandSite) moduleCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.moduleReqs[name]
}

func (s *demandSite) navs() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]string, len(s.navHdrs))
	copy(out, s.navHdrs)
	return out
}

// newDemandSite builds the rig. variant selects the boot page's
// markers: "outlet", "loading", "parts", or "vt".
func newDemandSite(t *testing.T, variant string) *demandSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &demandSite{
		moduleReqs: map[string]int{},
	}
	deferredField := ""
	if variant == "parts" || variant == "outlet+defer" {
		deferredField = `,"deferred":["l:site#aside"]`
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/"},` +
		`{"path":"/a","layouts":["l:site"]` + deferredField + `}` +
		`]</script>`
	var outlet, loadingTpl, vtKinds, deferNote string
	switch variant {
	case "outlet":
		outlet = `<div data-fui-outlet="l:site#aside" id="aside">OLD-ASIDE</div>`
	case "loading":
		// Isolated trigger: the template alone, no outlet marker, so
		// only the loading module's row fires.
		loadingTpl = `<template data-fui-loading="l:site#aside" data-fui-after="0" data-fui-min="0"><span id="skeleton">CLIENT-SKELETON</span></template>`
	case "outlet+defer":
		// The realistic deferred shape: outlets in the DOM AND the
		// manifest deferral, so the envelope navigator runs while the
		// parts module is the one being probed.
		outlet = `<div data-fui-outlet="l:site#aside" id="aside">OLD-ASIDE</div>`
	case "outlet+loading":
		outlet = `<div data-fui-outlet="l:site#aside" id="aside">OLD-ASIDE</div>`
		loadingTpl = `<template data-fui-loading="l:site#aside" data-fui-after="0" data-fui-min="0"><span id="skeleton">CLIENT-SKELETON</span></template>`
	case "parts":
		// Manifest-only trigger: no outlet marker in the DOM, the
		// route table alone carries a deferred address.
		deferNote = `<!-- deferred lives in the manifest only -->`
	case "vt":
		vtKinds = ` data-fui-vt-kinds="fade"`
	}
	shell := func(mainInner, aside string) string {
		out := outlet
		if aside != "" {
			out = `<div data-fui-outlet="l:site#aside" id="aside">` + aside + `</div>`
		}
		return `<!doctype html><html lang="en"` + vtKinds + `><head><title>demand</title>` + routes +
			`</head><body><div data-fui-layout="site" data-fui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a></nav>` + out +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:site" id="main">` + mainInner + `</main>` +
			`</div>` + loadingTpl + deferNote +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	// The counting twin of handleRuntimeModules: same route, same
	// sources, plus the per-module request log the assertions read.
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		s.bumpModule(name)
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			s.mu.Lock()
			s.navHdrs = append(s.navHdrs, map[string]string{
				"fills": r.Header.Get("X-Gofastr-Fills"),
				"defer": r.Header.Get("X-Gofastr-Defer"),
			})
			hold := s.pageHold
			s.mu.Unlock()
			if hold != nil {
				<-hold
			}
			// The fills envelope is negotiated: without X-Gofastr-Fills
			// the answer is a plain partial and the deferred outlet
			// renders INLINE (the server-side half of the spec's
			// before-load contract).
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "A")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			if r.Header.Get("X-Gofastr-Fills") == "2" {
				w.Header().Set("X-Gofastr-Envelope", "2")
				fmt.Fprint(w, `<template data-fui-fill="l:site">A-CONTENT</template>`+
					`<template data-fui-fill="l:site#aside">ENVELOPE-ASIDE</template>`)
				return
			}
			fmt.Fprint(w, `<p>A-CONTENT</p>`)
			return
		}
		// Full-document answers (a direct load, or the whole-document
		// fallback the plain navigator takes when the envelope module
		// fails to load): /a carries the A content and the aside's
		// fresh state, the shape the fallback's assertions read.
		if r.URL.Path == "/a" {
			fmt.Fprint(w, shell(`<p>A-CONTENT</p>`, `ENVELOPE-ASIDE`))
			return
		}
		fmt.Fprint(w, shell("HOME", ""))
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// blockModule arms CDP Fetch to fail one module's request (the spec's
// before-load probe). Only that URL pattern pauses; everything else
// flows untouched, so no continue plumbing is needed.
func blockModule(ctx context.Context, t *testing.T, name string) {
	t.Helper()
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		fs, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			_ = chromedp.Run(ctx, fetch.FailRequest(fs.RequestID, network.ErrorReasonConnectionFailed))
		}()
	})
	if err := chromedp.Run(ctx, fetch.Enable().WithPatterns([]*fetch.RequestPattern{
		{RequestStage: fetch.RequestStageRequest, URLPattern: `*__gofastr/runtime/` + name + `.js*`},
	})); err != nil {
		t.Fatalf("fetch.enable: %v", err)
	}
}

// TestDemandModuleTriggers pins the Opt-in table's trigger rows. The
// marker rows load exactly their own module, once, and nothing
// else's — EXCEPT the outlet marker, which since 2026-09-28 loads
// nothing at boot: an outlet costs a page nothing until its first
// navigation (the opt-in handoff, asserted in the outlet subtest
// below: boot requests nothing, the first click loads the envelope
// module exactly once and its fills apply).
func TestDemandModuleTriggers(t *testing.T) {
	for _, tc := range []struct {
		variant string
		want    string
	}{
		{"parts", "parts"},
		{"vt", "transition"},
	} {
		t.Run(tc.variant, func(t *testing.T) {
			s := newDemandSite(t, tc.variant)
			ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(s.srv.URL+"/"),
				chromedp.WaitVisible(`#goA`, chromedp.ByID),
			); err != nil {
				t.Fatalf("navigate: %v", err)
			}
			if err := chromedp.Run(ctx,
				chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules[`+fmt.Sprintf("%q", tc.want)+`]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
			); err != nil {
				t.Fatalf("module %s never loaded (requests: %v)", tc.want, s.moduleReqs)
			}
			// Exactly one request for it, none for the others.
			if n := s.moduleCount(tc.want); n != 1 {
				t.Errorf("module %s requested %d times, want exactly 1", tc.want, n)
			}
			for _, other := range []string{"envelope", "loading", "parts", "transition"} {
				if other == tc.want {
					continue
				}
				if n := s.moduleCount(other); n != 0 {
					t.Errorf("module %s requested %d times on a %s page, want 0", other, n, tc.variant)
				}
			}
		})
	}
	t.Run("loading", func(t *testing.T) {
		s := newDemandSite(t, "loading")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Sleep(300*time.Millisecond),
		); err != nil {
			t.Fatalf("navigate: %v", err)
		}
		// The loading module's scheduler is the ENVELOPE navigator's;
		// a template without an outlet marker is a page that can never
		// schedule it, and it loads NOTHING at boot (the trigger lives
		// in the envelope module's evaluation). Mutation it catches: a
		// boot row for the template makes this count 1.
		if n := s.moduleCount("loading"); n != 0 {
			t.Errorf("loading requested %d times at boot on a template-only page, want 0 (the scheduler is the envelope navigator's)", n)
		}
		for _, other := range []string{"envelope", "parts", "transition"} {
			if n := s.moduleCount(other); n != 0 {
				t.Errorf("module %s requested %d times on a template-only page, want 0", other, n)
			}
		}
	})
	t.Run("outlet+loading", func(t *testing.T) {
		s := newDemandSite(t, "outlet+loading")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Sleep(300*time.Millisecond),
		); err != nil {
			t.Fatalf("navigate: %v", err)
		}
		if n := s.moduleCount("loading"); n != 0 {
			t.Errorf("loading requested %d times at BOOT on a marker page, want 0 (the envelope module brings it at its evaluation)", n)
		}
		if err := chromedp.Run(ctx,
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["loading"] && !!window.__gofastr.loadedModules["envelope"]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("the first navigation loads the envelope module and, at its evaluation, the loading module: %v (requests: %v)", err, s.moduleReqs)
		}
		if n := s.moduleCount("loading"); n != 1 {
			t.Errorf("loading requested %d times, want exactly 1", n)
		}
	})
	t.Run("outlet", func(t *testing.T) {
		s := newDemandSite(t, "outlet")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
		); err != nil {
			t.Fatalf("navigate: %v", err)
		}
		// First paint requests NO layout module: the outlet marker is
		// not a boot trigger. Mutation it catches: restoring a boot
		// row for the marker makes this count 1.
		if n := s.moduleCount("envelope"); n != 0 {
			t.Errorf("envelope requested %d times at boot on an outlet page, want 0 (the outlet costs nothing until the first navigation)", n)
		}
		if err := chromedp.Run(ctx,
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["envelope"]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
			chromedp.Poll(`document.getElementById('aside').textContent === 'ENVELOPE-ASIDE'`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("first click loads the envelope module and applies the fills: %v (requests: %v)", err, s.moduleReqs)
		}
		if n := s.moduleCount("envelope"); n != 1 {
			t.Errorf("envelope requested %d times, want exactly 1", n)
		}
	})
}

// TestDemandModuleBeforeLoad pins the Opt-in table's before-load
// columns, per module, with the module's request failed through CDP
// Fetch:
//
//   - envelope blocked: the click STANDS DOWN (the marker page's
//     navigations belong to the module; the failed load falls back to
//     a WHOLE-DOCUMENT load of the destination, a bare fetch with no
//     negotiate headers): the content lands and the outlet takes the
//     fresh document's state — only the module can negotiate or apply
//     an envelope;
//   - parts blocked: the page request omits X-Gofastr-Defer, so the
//     deferred outlet renders inline and lands with the page;
//   - transition blocked: the navigation still lands (no transition:
//     document.startViewTransition is never called, no crash, no
//     gofastr:transition event);
//   - loading blocked: the busy dim alone — the region is marked
//     aria-busy while in flight, no loading clone parks.
func TestDemandModuleBeforeLoad(t *testing.T) {
	t.Run("envelope", func(t *testing.T) {
		s := newDemandSite(t, "outlet")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		blockModule(ctx, t, "envelope")
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`document.getElementById('main').textContent.includes('A-CONTENT')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("whole-document fallback with envelope blocked: %v", err)
		}
		if navs := s.navs(); len(navs) != 0 {
			t.Errorf("%d navigation requests reached the server with the envelope module unavailable (the stand-down owns the negotiation; the fallback is a bare whole-document fetch)", len(navs))
		}
		var aside string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('aside').textContent`, &aside))
		if aside != "ENVELOPE-ASIDE" {
			t.Errorf("outlet content = %q after the whole-document fallback, want the fresh document's ENVELOPE-ASIDE", aside)
		}
	})
	t.Run("parts", func(t *testing.T) {
		s := newDemandSite(t, "outlet+defer")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		blockModule(ctx, t, "parts")
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`document.getElementById('main').textContent.includes('A-CONTENT')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("navigation with parts blocked: %v", err)
		}
		for i, n := range s.navs() {
			if n["defer"] != "" {
				t.Errorf("navigation %d sent X-Gofastr-Defer=%q with the parts module unavailable", i, n["defer"])
			}
		}
	})
	t.Run("transition", func(t *testing.T) {
		s := newDemandSite(t, "vt")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		blockModule(ctx, t, "transition")
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
		); err != nil {
			t.Fatalf("navigate: %v", err)
		}
		// Arm on the REAL page, before the click. The old shape of
		// this guard installed the listener before Navigate and so
		// observed only about:blank — vacuously green while core
		// still wrapped every swap in a view transition. Beside the
		// event counter, wrap document.startViewTransition itself: a
		// navigation on a page the module never reached must call it
		// zero times, the pre-layout behavior.
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			window.__vtEvents = 0;
			document.addEventListener('gofastr:transition', () => window.__vtEvents++);
			window.__svtCalls = 0;
			const orig = document.startViewTransition;
			if (orig) document.startViewTransition = function (cb) { window.__svtCalls++; return orig.call(this, cb); };
			return true;
		})()`, nil)); err != nil {
			t.Fatalf("arm transition probes: %v", err)
		}
		if err := chromedp.Run(ctx,
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`document.getElementById('main').textContent.includes('A-CONTENT')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("navigation with transition blocked: %v", err)
		}
		var counts []float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(`[window.__vtEvents, window.__svtCalls]`, &counts)); err != nil {
			t.Fatalf("read transition probes: %v", err)
		}
		if len(counts) != 2 || counts[0] != 0 {
			t.Errorf("gofastr:transition fired %v times with the transition module unavailable", counts[0])
		}
		if len(counts) != 2 || counts[1] != 0 {
			t.Errorf("document.startViewTransition called %v times with the transition module unavailable — before the module loads the swap applies directly", counts[1])
		}
	})
	t.Run("loading", func(t *testing.T) {
		s := newDemandSite(t, "outlet+loading")
		ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
		blockModule(ctx, t, "loading")
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
		); err != nil {
			t.Fatalf("navigate: %v", err)
		}
		// Hold the page answer in flight: since the opt-in handoff the
		// marks arm when the envelope module lands beside the fetch,
		// so the busy window needs a genuinely slow answer to be
		// observable. The region is marked, and no clone ever parks:
		// the skeleton must not appear (the loading module owns the
		// clone).
		s.holdPages()
		if err := chromedp.Run(ctx, chromedp.Click(`#goA`, chromedp.ByID)); err != nil {
			t.Fatalf("click: %v", err)
		}
		if err := chromedp.Run(ctx,
			chromedp.Poll(`document.getElementById('aside').getAttribute('aria-busy') === 'true'`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
		); err != nil {
			s.releasePages()
			t.Fatal("the busy dim never showed while in flight")
		}
		s.releasePages()
		var skeleton string
		if err := chromedp.Run(ctx,
			chromedp.Poll(`document.getElementById('main').textContent.includes('A-CONTENT')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("navigation never landed: %v", err)
		}
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(document.getElementById('skeleton') !== null) ? 'SKELETON-APPEARED' : 'no-skeleton'`, &skeleton))
		if skeleton != "no-skeleton" {
			t.Errorf("loading content appeared with the loading module unavailable: %s", skeleton)
		}
	})
}

// TestDeferredMarkerPageBootLoadsEnvelope pins the deferred trigger's
// module-side half (the address walk at the parts module's
// evaluation): a page whose manifest defers addresses that are LIVE
// in its DOM boot-loads the envelope module at first paint, so its
// first navigation already runs the module navigator
// (X-Gofastr-Defer beside the page fetch, no hand-off round-trip).
// Mutation it catches: dropping the address walk in src/parts.js
// leaves the module to the first click's hand-off and the boot-time
// request never happens.
func TestDeferredMarkerPageBootLoadsEnvelope(t *testing.T) {
	s := newDemandSite(t, "outlet+defer")
	ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["envelope"]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("envelope never boot-loaded on a deferred marker page (requests: %v)", s.moduleReqs)
	}
	if n := s.moduleCount("envelope"); n != 1 {
		t.Errorf("envelope requested %d times at boot, want exactly 1", n)
	}
	if n := s.moduleCount("parts"); n != 1 {
		t.Errorf("parts requested %d times at boot, want exactly 1", n)
	}
}
