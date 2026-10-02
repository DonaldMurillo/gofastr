package uihost

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// plainSiteComp renders a paragraph whose id is derived from its
// marker (unique per surface: the zone layout's header and its screen
// both render one) plus nav links, so a click navigation between the
// pages is one query away.
type plainSiteComp struct {
	marker string
	links  [][2]string
}

func (c *plainSiteComp) Render() render.HTML { return c.RenderCtx(context.Background()) }

func (c *plainSiteComp) RenderCtx(_ context.Context) render.HTML {
	var b strings.Builder
	fmt.Fprintf(&b, `<p id="mk-%s">%s</p>`, c.marker, c.marker)
	for _, l := range c.links {
		fmt.Fprintf(&b, ` <a id="%s" href="%s">%s</a>`, l[0], l[1], l[1])
	}
	return render.HTML(b.String())
}

// TestPlainSiteShipsCoreOnly pins the opt-in decision
// (docs/DESIGN-layout-outlets.md "### Opt-in", Donald 2026-09-26): a
// marketing site or a blog — pages with no layout, or a static-cell
// layout with no outlets, areas, loading templates, deferred outlets
// or transition vocabulary — uses none of the layout machinery and
// pays nothing for it.
//
// Asserted over CDP on a real app-host pair of pages:
//   - no demand-module request for any of the four layout modules;
//   - a click navigation sends no X-Gofastr-Fills header;
//   - the server ran no resolver and no deferred work (counters);
//   - the served core runtime.js sits inside the itemised budget
//     line (the byte half; see the assertion at the tail).
//
// The mutation it catches (per the spec's test table): loading the
// modules eagerly. A marker scan that ignored its selector, a boot
// trigger without a gate, or an unconditional Fills header all fail
// one of the three assertions; un-itemised core growth fails the
// fourth.
func TestPlainSiteShipsCoreOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	_ = browserExecutable(t)

	var mu sync.Mutex
	moduleReqs := map[string]int{}
	fillsHdrs := 0
	resolverRuns := 0

	application := app.NewApp("PlainSite")

	home := &plainSiteComp{marker: "HOME", links: [][2]string{{"goAbout", "/about"}, {"goZone", "/zone/a"}}}
	about := &plainSiteComp{marker: "ABOUT", links: [][2]string{{"goHome", "/"}}}
	application.RegisterScreen(app.NewScreen("/", home).WithTitle("Home"), nil)
	application.RegisterScreen(app.NewScreen("/about", about).WithTitle("About"), nil)

	// A static-cell layout: a real ScreenGroup with a layout wrapper
	// and header chrome — and no outlets, no areas, no loading, no
	// deferral, no transition vocabulary. Its pages must behave
	// exactly like the layout-less ones. The resolver below is never
	// read by anything this test navigates to; its counter is the
	// "no resolution ran" proof.
	zone := app.NewScreenGroup("/zone", headerLayout("zone", &plainSiteComp{marker: "ZONE-HEADER"}))
	zone.Screen(app.NewScreen("/zone/a", &plainSiteComp{marker: "ZONE-A", links: [][2]string{{"goHome2", "/"}}}).WithTitle("Zone A"), nil)
	zone.Resolve(app.NewKey[string]("never").From(func(_ context.Context) (string, error) {
		mu.Lock()
		resolverRuns++
		mu.Unlock()
		return "resolved", nil
	}))
	application.Router.ScreenGroup(zone)

	host := New(application)

	obs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__gofastr/runtime/") {
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
			mu.Lock()
			moduleReqs[name]++
			mu.Unlock()
		}
		if r.Header.Get("X-Gofastr-Navigate") == "1" && r.Header.Get("X-Gofastr-Fills") != "" {
			mu.Lock()
			fillsHdrs++
			mu.Unlock()
		}
		host.ServeHTTP(w, r)
	}))
	t.Cleanup(obs.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	_ = chromedp.Run(ctx, network.Enable())

	if err := chromedp.Run(ctx,
		chromedp.Navigate(obs.URL+"/"),
		chromedp.WaitVisible(`#goAbout`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate home: %v", err)
	}
	// View transitions are opt-in (the corrected Opt-in table's
	// before-load column): a plain page declares no transition, so a
	// click must start NONE — document.startViewTransition is wrapped
	// before the first click so a call is observable, and the
	// gofastr:transition event is counted. The runtime once wrapped
	// every swap in a view transition unconditionally; this is the
	// guard against that returning.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__vtEvents = 0;
		document.addEventListener('gofastr:transition', () => window.__vtEvents++);
		window.__svtCalls = 0;
		const orig = document.startViewTransition;
		if (orig) document.startViewTransition = function (cb) { window.__svtCalls++; return orig.call(this, cb); };
		return true;
	})()`, nil)); err != nil {
		t.Fatalf("arm view-transition probes: %v", err)
	}
	// Click between the two plain pages, then into and out of the
	// static-cell layout. Every one of these must be a plain partial.
	for _, step := range []struct{ link, marker string }{
		{"#goAbout", "ABOUT"},
		{"#goHome", "HOME"},
		{"#goZone", "ZONE-A"},
		{"#goHome2", "HOME"},
	} {
		var ok bool
		if err := chromedp.Run(ctx,
			chromedp.Click(step.link, chromedp.ByID),
			chromedp.Poll(`document.getElementById('mk-`+step.marker+`') !== null`, &ok, chromedp.WithPollingTimeout(15*time.Second)),
		); err != nil {
			t.Fatalf("click %s: %v", step.link, err)
		}
	}

	var vtCounts []float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[window.__vtEvents, window.__svtCalls]`, &vtCounts)); err != nil {
		t.Fatalf("read view-transition probes: %v", err)
	}
	if len(vtCounts) != 2 || vtCounts[0] != 0 || vtCounts[1] != 0 {
		t.Errorf("plain-page clicks started %v view transitions and fired %v gofastr:transition events; a page that declares none must start none", vtCounts[1], vtCounts[0])
	}
	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"envelope", "loading", "parts", "transition"} {
		if moduleReqs[name] != 0 {
			t.Errorf("layout module %q was requested %d times on a plain site — the opt-in is broken", name, moduleReqs[name])
		}
	}
	if fillsHdrs != 0 {
		t.Errorf("%d click navigations sent X-Gofastr-Fills on a page with no outlet/area marker", fillsHdrs)
	}
	if resolverRuns != 0 {
		t.Errorf("resolver ran %d times on navigations that never read it", resolverRuns)
	}
	// The byte half: the core this plain page loaded — fetched from
	// the same server the browser used, so the SERVED artifact is
	// what is measured — sits inside the itemised budget line (the
	// derivation lives in core-ui/runtime/budget_test.go: pre-layout
	// 12718 plus the measured, named pieces plus clearance;
	// TestPlainSiteLineMatchesCoreBudget there keeps this copy in step).
	// Mutation it catches: a carve that regresses, or an un-itemised
	// addition to frag/*.js, ships bytes to every plain page and
	// crosses the line here first.
	const coreLineGZ = 13510
	resp, err := http.Get(obs.URL + "/__gofastr/runtime.js")
	if err != nil {
		t.Fatalf("fetch runtime.js: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read runtime.js: %v", err)
	}
	if got := gzip6(t, body); got > coreLineGZ {
		t.Errorf("served core runtime.js gzip = %d > itemised line %d — plain pages pay for core bytes nothing itemises; see core-ui/runtime/budget_test.go's derivation", got, coreLineGZ)
	}
}

// gzip6 compresses src at level 6 — the budget line's ruler (see
// core-ui/runtime/budget_test.go's gzipSize for the canonical copy).
func gzip6(t *testing.T, src []byte) int {
	t.Helper()
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(src)
	_ = w.Close()
	return buf.Len()
}
