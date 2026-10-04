package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	toolbarSel = `[data-cui-outlet="l:shell#toolbar"]`
	asideSel   = `[data-cui-outlet="l:shell#aside"]`
	crumbsSel  = `[data-cui-area="l:shell~crumbs"]`
)

func labBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	root, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	if err := chromedp.Run(root); err != nil {
		t.Fatalf("browser failed to start: %v", err)
	}
	return root
}

// labRead reads the trimmed text of sel. A missing element reads as
// "!missing" so the failure is visible without embedding the selector
// (whose quotes would break the expression).
func labRead(t *testing.T, ctx context.Context, sel string) string {
	t.Helper()
	var out string
	expr := `(() => { const el = document.querySelector('` + sel + `'); ` +
		`return el ? el.textContent.trim() : '!missing'; })()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out)); err != nil {
		t.Fatalf("read %s: %v", sel, err)
	}
	return out
}

func labNavigations(t *testing.T, base string) int64 {
	t.Helper()
	res, err := http.Get(base + "/__lab/stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	defer res.Body.Close()
	var stats struct {
		Navigations int64 `json:"navigations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
		t.Fatalf("stats decode: %v", err)
	}
	return stats.Navigations
}

// labState asserts the three fills-carried regions plus the primary
// marker's presence (waits for it first).
func labState(t *testing.T, ctx context.Context, primary, toolbar, aside, crumbs string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.WaitVisible(`#lab-`+primary, chromedp.ByQuery),
		// The fills apply in the same task as the primary swap, but let
		// the (hash-skipped or applied) outlets settle before reading.
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("wait %s: %v", primary, err)
	}
	if got := labRead(t, ctx, toolbarSel); got != toolbar {
		t.Errorf("toolbar = %q, want %q", got, toolbar)
	}
	if got := labRead(t, ctx, asideSel); got != aside {
		t.Errorf("aside = %q, want %q", got, aside)
	}
	if got := labRead(t, ctx, crumbsSel); got != crumbs {
		t.Errorf("crumbs = %q, want %q", got, crumbs)
	}
	if got := labRead(t, ctx, `main[data-cui-layout-slot="l:shell"]`); !strings.Contains(got, primary) {
		t.Errorf("primary slot = %q, want it to contain %q", got, primary)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestTreeLayoutFillsE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL

	browser := labBrowserCtx(t)
	tab, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	ctx, tcancel := context.WithTimeout(tab, 120*time.Second)
	t.Cleanup(tcancel)

	// /: no fills. Toolbar empty (FallbackNothing), aside is the
	// default HelpPanel, crumbs render the path.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#lab-SCREEN-HOME`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate home: %v", err)
	}
	labState(t, ctx, "SCREEN-HOME", "", "ASIDE-HELP", "crumbs:/")

	// Stamp a property on the kept static header: as long as the shell
	// layer is kept, the ELEMENT survives with its identity and any
	// expando state.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('shell-header').__labProbe = 'alive'`, nil)); err != nil {
		t.Fatalf("stamp probe: %v", err)
	}

	navs0 := labNavigations(t, base)

	// -> /inbox/1: toolbar and aside are the screen's fills.
	labSkipVT(t, ctx)
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.Click(`a[data-nav="/inbox/1"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click message: %v", err)
	}
	labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
	if got := labRead(t, ctx, "#lab-crumbs-bind"); !strings.HasSuffix(got, "1") {
		t.Errorf("route-bound crumbs did not reflect the message route: %q", got)
	}
	if got := labRead(t, ctx, "#lab-timeline"); !strings.Contains(got, "SCREEN-MESSAGE-1") {
		t.Errorf("timeline did not report the changed message region: %q", got)
	}
	var probe string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('shell-header').__labProbe || 'gone'`, &probe)); err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if probe != "alive" {
		t.Fatalf("static header did not survive the fills navigation (probe=%q)", probe)
	}
	navs1 := labNavigations(t, base)
	if navs1 != navs0+1 {
		t.Fatalf("navigations after click 1 = %d, want %d (exactly one partial)", navs1, navs0+1)
	}

	// -> /settings: no fills; both outlets fall back, crumbs re-run.
	labSkipVT(t, ctx)
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.Click(`a[data-nav="/settings"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click settings: %v", err)
	}
	labState(t, ctx, "SCREEN-SETTINGS", "", "ASIDE-HELP", "crumbs:/settings")
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('shell-header').__labProbe || 'gone'`, &probe)); err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if probe != "alive" {
		t.Fatalf("static header did not survive the settings navigation (probe=%q)", probe)
	}
	navs2 := labNavigations(t, base)
	if navs2 != navs1+1 {
		t.Fatalf("navigations after click 2 = %d, want %d", navs2, navs1+1)
	}

	// Back: replays the cached /inbox/1 snapshot — fills included, and
	// NO new navigation request.
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.Evaluate(`history.back()`, nil),
	); err != nil {
		t.Fatalf("history.back: %v", err)
	}
	labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('shell-header').__labProbe || 'gone'`, &probe)); err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if probe != "alive" {
		t.Fatalf("static header did not survive the back replay (probe=%q)", probe)
	}
	navs3 := labNavigations(t, base)
	if navs3 != navs2 {
		t.Fatalf("Back fetched again (%d -> %d); a cache replay must not hit the server", navs2, navs3)
	}
}

// newLabTab returns a fresh tab: the screen cache is per-document JS
// state, so a new tab's first navigation is a genuine full load and its
// boot capture is exercised.
func newLabTab(t *testing.T, browser context.Context) context.Context {
	t.Helper()
	tab, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	ctx, tcancel := context.WithTimeout(tab, 120*time.Second)
	t.Cleanup(tcancel)
	return ctx
}

func labFullLoad(t *testing.T, ctx context.Context, base, path, primary string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+path),
		chromedp.WaitVisible(`#lab-`+primary, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("full-load %s: %v", path, err)
	}
}

// labSkipVT ends any running view transition: while one is active the
// snapshot layer owns hit-testing and a coordinate click would be lost
// (see labClickNav's note).
func labSkipVT(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`if (document.activeViewTransition) document.activeViewTransition.skipTransition(); true`, nil)); err != nil {
		t.Fatalf("skip active transition: %v", err)
	}
}

func labClickNav(t *testing.T, ctx context.Context, href string) {
	t.Helper()
	// P11 (spike/layout-motion): while a view transition is active the
	// transition's snapshot layer owns hit-testing — Chrome delivers no
	// pointer input to the page (measured: elementFromPoint at a link
	// returns <html>; a coordinate click mid-transition is lost, and
	// pointer-events:none on the pseudos does not restore it). Skip any
	// running transition before the coordinate click so the click lands;
	// the wrapper's own skip-on-second-navigation path is exercised by
	// the popstate/programmatic steps of the P11 tests.
	labSkipVT(t, ctx)
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.Click(`a[data-nav="`+href+`"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click %s: %v", href, err)
	}
}

func labBack(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.Evaluate(`history.back()`, nil),
	); err != nil {
		t.Fatalf("history.back: %v", err)
	}
}

// TestTreeLayoutBootCaptureE2E pins the BOOT and FULL-DOCUMENT captures:
// a page whose first paint was a full load (not a partial) must replay
// its fills from the capture, not from whatever the outlets held when
// Back returns to it.
func TestTreeLayoutBootCaptureE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL
	browser := labBrowserCtx(t)

	t.Run("boot capture replays fills on Back", func(t *testing.T) {
		ctx := newLabTab(t, browser)
		labFullLoad(t, ctx, base, "/inbox", "SCREEN-INBOX")
		labState(t, ctx, "SCREEN-INBOX", "TOOLBAR-INBOX", "ASIDE-HELP", "crumbs:/inbox")
		labClickNav(t, ctx, "/inbox/1")
		labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
		labBack(t, ctx)
		labState(t, ctx, "SCREEN-INBOX", "TOOLBAR-INBOX", "ASIDE-HELP", "crumbs:/inbox")
	})

	t.Run("boot capture of a filled page replays on Back", func(t *testing.T) {
		ctx := newLabTab(t, browser)
		labFullLoad(t, ctx, base, "/inbox/1", "SCREEN-MESSAGE-1")
		labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
		labClickNav(t, ctx, "/settings")
		labState(t, ctx, "SCREEN-SETTINGS", "", "ASIDE-HELP", "crumbs:/settings")
		labBack(t, ctx)
		labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
	})

	t.Run("Back twice returns to the boot captured fills", func(t *testing.T) {
		ctx := newLabTab(t, browser)
		labFullLoad(t, ctx, base, "/settings", "SCREEN-SETTINGS")
		labState(t, ctx, "SCREEN-SETTINGS", "", "ASIDE-HELP", "crumbs:/settings")
		labClickNav(t, ctx, "/inbox/1")
		labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
		labClickNav(t, ctx, "/inbox")
		labState(t, ctx, "SCREEN-INBOX", "TOOLBAR-INBOX", "ASIDE-HELP", "crumbs:/inbox")
		labBack(t, ctx)
		labState(t, ctx, "SCREEN-MESSAGE-1", "TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1")
		labBack(t, ctx)
		labState(t, ctx, "SCREEN-SETTINGS", "", "ASIDE-HELP", "crumbs:/settings")
	})
}

// TestNavBusyPageWideE2E pins P4 variant A (today): while a slow SPA
// navigation is in flight the ONLY busy marking is the page-wide
// aria-busy on <html> (the progress strip's CSS hook); no outlet,
// area, or slot element is marked, and the flag clears on apply.
func TestNavBusyPageWideE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Slow navigation: the /slow/900 fill's Load sleeps 900ms, so the
	// fetch is still in flight right after the navigate() dispatch
	// returns (the nav header links /slow/300 only; programmatic
	// navigation drives the same loadPage path).
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/900')`, nil),
	); err != nil {
		t.Fatalf("navigate slow: %v", err)
	}
	var probe struct {
		HTMLBusy     string `json:"htmlBusy"`
		Marked       int    `json:"marked"`
		StripContent string `json:"strip"`
	}
	// The per-region marks arm when the envelope module lands beside
	// the fetch (an outlet page no longer boot-loads it — the opt-in
	// decision of 2026-09-28), so the probe POLLS for them instead of
	// sampling once: they must be up well inside the 900ms flight.
	if err := chromedp.Run(ctx,
		chromedp.Poll(`(() => {
			let marked = 0;
			for (const el of document.querySelectorAll('[data-cui-outlet],[data-cui-area],[data-cui-layout-slot]')) {
				if (el.getAttribute('aria-busy') === 'true') marked++;
			}
			return marked === 5 && document.documentElement.getAttribute('aria-busy') === 'true';
		})()`, new(bool), chromedp.WithPollingTimeout(500*time.Millisecond)),
		chromedp.Evaluate(`(() => {
			let marked = 0;
			for (const el of document.querySelectorAll('[data-cui-outlet],[data-cui-area],[data-cui-layout-slot]')) {
				if (el.getAttribute('aria-busy') === 'true') marked++;
			}
			return {
				htmlBusy: document.documentElement.getAttribute('aria-busy'),
				marked,
				strip: getComputedStyle(document.documentElement, '::after').content,
			};
		})()`, &probe),
	); err != nil {
		t.Fatalf("probe during flight: %v", err)
	}
	if probe.HTMLBusy != "true" {
		t.Fatalf("html aria-busy during flight = %q, want \"true\"", probe.HTMLBusy)
	}
	// Under variant B (this tree) the kept layers' regions are ALSO
	// marked; the pure page-wide-only behaviour is pinned at the P4-A
	// commit. Here: html marked, and every kept-layer region marked.
	if probe.Marked != 5 {
		t.Fatalf("%d outlet/area/slot elements carried aria-busy during flight, want 5 (toolbar, aside, rail, crumbs, swap slot)", probe.Marked)
	}
	if probe.StripContent == "none" {
		t.Fatalf("the progress strip's ::after rule did not apply while html[aria-busy=true] (computed content=%q); frameworkBuiltinCSS missing?", probe.StripContent)
	}

	// Settle: the flag clears and the slow fill applied. The || ''
	// coercion matters: getAttribute returns null once cleared, and a
	// JSON null does not overwrite the Go string (it would keep the
	// stale "true" from the flight probe and read as never cleared).
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-900`, chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.documentElement.getAttribute('aria-busy') || ''`, &probe.HTMLBusy),
	); err != nil {
		t.Fatalf("wait settle: %v", err)
	}
	if probe.HTMLBusy != "" {
		t.Fatalf("html aria-busy after settle = %q, want cleared", probe.HTMLBusy)
	}
	labState(t, ctx, "SCREEN-SLOW-900", "", "ASIDE-SLOW-900", "crumbs:/slow/900")
}

// TestLayoutlabWithoutJS is the JavaScript-off gate: every page is
// fully server-rendered, so plain HTTP GETs carry the markers, and no
// served markup ships an aria-busy (busy state is runtime-only).
func TestLayoutlabWithoutJS(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL
	for _, tc := range []struct{ path, want string }{
		{"/", "SCREEN-HOME"},
		{"/inbox", "SCREEN-INBOX"},
		{"/inbox/1", "SCREEN-MESSAGE-1"},
		{"/settings", "SCREEN-SETTINGS"},
		{"/slow/300", "SCREEN-SLOW-300"},
		{"/first/abc", "SCREEN-FIRST-abc"},
		{"/bare", "SCREEN-BARE"},
	} {
		res, err := http.Get(base + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		page := string(body)
		if !strings.Contains(page, tc.want) {
			t.Errorf("GET %s: SSR body lacks %q", tc.path, tc.want)
		}
		if strings.Contains(page, "aria-busy") {
			t.Errorf("GET %s: SSR body ships aria-busy; busy state must be runtime-only", tc.path)
		}
	}
	// The fills ride the SSR body too: /inbox/1 fills toolbar + aside.
	res, err := http.Get(base + "/inbox/1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, want := range []string{"TOOLBAR-MESSAGE", "ASIDE-SENDER-1", "crumbs:/inbox/1"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("GET /inbox/1 SSR body lacks %q", want)
		}
	}
	// P7: the route bindings' first paint is server-stamped (JS-off
	// shows the same values the signals would publish).
	for _, want := range []string{
		`data-cui-signal="route.title" id="lab-route-title">Message 1<`,
		`data-cui-signal="route.params.id" id="lab-route-param">1<`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("GET /inbox/1 SSR body lacks the stamped route binding %q", want)
		}
	}
}

// TestNavBusyPerRegionE2E pins P4 variant B: during a slow navigation
// every outlet and area of the kept layers plus the swap slot carry
// aria-busy="true", the theme CSS's transition-delayed dim actually
// paints for a slow response, a fast (<100ms) response paints ZERO
// dimmed frames, and every mark clears on apply.
func TestNavBusyPerRegionE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 90*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// rAF sampler: count frames in which ANY of the markable regions
	// has a painted dim (computed opacity below 1).
	installSampler := func(name string) error {
		return chromedp.Run(tctx, chromedp.Evaluate(`(() => {
			window[`+name+`] = { dimmed: 0, frames: 0, done: false };
			const tick = () => {
				let dim = false;
				for (const el of document.querySelectorAll('[data-cui-outlet],[data-cui-area],[data-cui-layout-slot]')) {
					if (parseFloat(getComputedStyle(el).opacity) < 0.999) { dim = true; break; }
				}
				const s = window[`+name+`];
				s.frames++;
				if (dim) s.dimmed++;
				if (!s.done) requestAnimationFrame(tick);
			};
			requestAnimationFrame(tick);
			return true;
		})()`, nil))
	}

	// --- slow navigation: marked + dim paints ---
	if err := installSampler(`'__s1'`); err != nil {
		t.Fatalf("sampler: %v", err)
	}
	if err := chromedp.Run(tctx, page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/900')`, nil)); err != nil {
		t.Fatalf("navigate slow: %v", err)
	}
	var probe struct {
		Toolbar string `json:"toolbar"`
		Aside   string `json:"aside"`
		Crumbs  string `json:"crumbs"`
		Slot    string `json:"slot"`
	}
	// The marks arm when the envelope module lands beside the fetch (an
	// outlet page no longer boot-loads it), so poll for them inside the
	// 900ms flight before sampling, as TestNavBusyPageWideE2E does.
	// A timeout here falls through to the sample, which names the region.
	_ = chromedp.Run(tctx, chromedp.Poll(`['[data-cui-outlet="l:shell#toolbar"]', '[data-cui-outlet="l:shell#aside"]', '[data-cui-area="l:shell~crumbs"]', 'main[data-cui-layout-slot="l:shell"]']
		.every((s) => document.querySelector(s)?.getAttribute('aria-busy') === 'true')`,
		new(bool), chromedp.WithPollingTimeout(500*time.Millisecond)))
	if err := chromedp.Run(tctx, chromedp.Evaluate(`(() => {
		const g = (s) => document.querySelector(s).getAttribute('aria-busy') || '';
		return {
			toolbar: g('[data-cui-outlet="l:shell#toolbar"]'),
			aside:   g('[data-cui-outlet="l:shell#aside"]'),
			crumbs:  g('[data-cui-area="l:shell~crumbs"]'),
			slot:    g('main[data-cui-layout-slot="l:shell"]'),
		};
	})()`, &probe)); err != nil {
		t.Fatalf("probe during flight: %v", err)
	}
	for name, got := range map[string]string{"toolbar": probe.Toolbar, "aside": probe.Aside, "crumbs": probe.Crumbs, "swap slot": probe.Slot} {
		if got != "true" {
			t.Errorf("during slow flight, %s aria-busy = %q, want \"true\"", name, got)
		}
	}
	// The old content stays visible while marked (dim, not removal).
	if got := labRead(t, tctx, asideSel); got != "ASIDE-HELP" {
		t.Errorf("during slow flight, aside content = %q, want the old ASIDE-HELP still visible", got)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-900`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond), // let the sampler collect the dimmed frames
		chromedp.Evaluate(`window.__s1.done = true`, nil),
		chromedp.Evaluate(`JSON.stringify(window.__s1)`, &probe.Toolbar),
	); err != nil {
		t.Fatalf("settle slow: %v", err)
	}
	var slowSampler struct {
		Dimmed int `json:"dimmed"`
		Frames int `json:"frames"`
	}
	if err := json.Unmarshal([]byte(probe.Toolbar), &slowSampler); err != nil {
		t.Fatalf("sampler json: %v", err)
	}
	if slowSampler.Dimmed == 0 {
		t.Errorf("slow navigation painted 0 dimmed frames; the delayed dim never showed (frames=%d)", slowSampler.Frames)
	}
	labState(t, tctx, "SCREEN-SLOW-900", "", "ASIDE-SLOW-900", "crumbs:/slow/900")

	if err := chromedp.Run(tctx, chromedp.Evaluate(`(() => {
		let n = 0;
		for (const el of document.querySelectorAll('[aria-busy="true"]')) n++;
		return String(n);
	})()`, &probe.Toolbar)); err != nil {
		t.Fatalf("clear probe: %v", err)
	}
	if probe.Toolbar != "0" {
		t.Errorf("%s elements still aria-busy after apply", probe.Toolbar)
	}

	// --- fast navigation: zero dimmed frames ---
	if err := installSampler(`'__s2'`); err != nil {
		t.Fatalf("sampler 2: %v", err)
	}
	if err := chromedp.Run(tctx, page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/settings')`, nil)); err != nil {
		t.Fatalf("navigate fast: %v", err)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__s2.done = true`, nil),
		chromedp.Evaluate(`JSON.stringify(window.__s2)`, &probe.Toolbar),
	); err != nil {
		t.Fatalf("settle fast: %v", err)
	}
	var fastSampler struct {
		Dimmed int `json:"dimmed"`
		Frames int `json:"frames"`
	}
	if err := json.Unmarshal([]byte(probe.Toolbar), &fastSampler); err != nil {
		t.Fatalf("sampler2 json: %v", err)
	}
	if fastSampler.Dimmed != 0 {
		t.Errorf("fast navigation painted %d dimmed frames; the transition delay must keep fast (<100ms) responses flicker-free (frames=%d)", fastSampler.Dimmed, fastSampler.Frames)
	}
}

// labRoute reads the four route-bound header elements (P7).
func labRoute(t *testing.T, ctx context.Context) (title, param, crumbs, mix string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`JSON.stringify({
			t: document.getElementById('lab-route-title').textContent,
			p: document.getElementById('lab-route-param').textContent,
			c: document.getElementById('lab-crumbs-bind').textContent,
			m: document.getElementById('lab-mix-bind').textContent,
		})`, &title),
	); err != nil {
		t.Fatalf("read route bindings: %v", err)
	}
	var v struct {
		T string `json:"t"`
		P string `json:"p"`
		C string `json:"c"`
		M string `json:"m"`
	}
	if err := json.Unmarshal([]byte(title), &v); err != nil {
		t.Fatalf("route bindings json: %v", err)
	}
	return v.T, v.P, v.C, v.M
}

// TestRouteSignalsAtomicMergeE2E pins P7 variant C: seeded route slices
// (as B) with an ATOMIC page-scoped merge — every changed value installs
// silently first, then each changed key notifies once, so a computed over
// two route values never sees a mid-merge mix (B measured one mixed
// evaluation per navigation; A measured zero via its island module).
// Also pins C's first-render rule: a param slice declared by the very
// render that first carries its value stamps it (B stamped ”).
func TestRouteSignalsAtomicMergeE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 90*time.Second)
	t.Cleanup(tcancel)

	// First paint is server-rendered (works with JS off too).
	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/inbox/1"),
		chromedp.WaitVisible(`#lab-SCREEN-MESSAGE-1`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	title, param, crumbs, mix := labRoute(t, tctx)
	if title != "Message 1" || param != "1" || crumbs != "Home / Inbox / 1" || mix != "/inbox/1|1" {
		t.Fatalf("SSR route bindings = (%q,%q,%q,%q), want (Message 1,1,Home / Inbox / 1,/inbox/1|1)", title, param, crumbs, mix)
	}

	// Record EVERY lab.mix evaluation: the reducer runs on any dep
	// change, so a mixed-route pair (path from the new route, id from
	// the old) is visible here and nowhere else.
	if err := chromedp.Run(tctx, chromedp.Evaluate(`(() => {
		const G = window.__gofastr;
		G._reducers['lab.mix'] = function (deps) {
			const s = (deps['route.path'] || '') + '|' + (deps['route.params.id'] || '');
			(window.__mixRec = window.__mixRec || []).push(s);
			return s;
		};
		window.__mixRec = [];
		return true;
	})()`, nil)); err != nil {
		t.Fatalf("install recorder: %v", err)
	}
	if err := chromedp.Run(tctx, page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/inbox/2')`, nil)); err != nil {
		t.Fatalf("navigate /inbox/2: %v", err)
	}
	// Navigate /inbox/1 -> /inbox/2 (envelope path): all four update.
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-MESSAGE-2`, chromedp.ByQuery),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("wait message 2: %v", err)
	}
	title, param, crumbs, mix = labRoute(t, tctx)
	if title != "Message 2" || param != "2" || crumbs != "Home / Inbox / 2" || mix != "/inbox/2|2" {
		t.Fatalf("after nav, route bindings = (%q,%q,%q,%q), want (Message 2,2,Home / Inbox / 2,/inbox/2|2)", title, param, crumbs, mix)
	}
	var rec []string
	if err := chromedp.Run(tctx, chromedp.Evaluate(`JSON.stringify(window.__mixRec || [])`, &title)); err != nil {
		t.Fatalf("read recorder: %v", err)
	}
	if err := json.Unmarshal([]byte(title), &rec); err != nil {
		t.Fatalf("recorder json: %v", err)
	}
	mixed := 0
	for _, e := range rec {
		parts := strings.SplitN(e, "|", 2)
		if len(parts) == 2 && parts[1] != "" && !strings.HasSuffix(parts[0], "/"+parts[1]) {
			mixed++
		}
	}
	t.Logf("P7-C recorded %d computed evaluations, %d of them mid-merge mixes (path and id from different routes): %v", len(rec), mixed, rec)
	if len(rec) == 0 {
		t.Errorf("the recorder saw no computed evaluations; the measurement is vacuous")
	}
	if mixed != 0 {
		t.Errorf("%d mid-merge mixes; the atomic install-then-notify merge must make every evaluation single-route", mixed)
	}
	if last := rec[len(rec)-1]; last != "/inbox/2|2" {
		t.Errorf("final computed evaluation = %q, want the settled \"/inbox/2|2\"", last)
	}

	// Navigate to a route without the param: the previous id must not
	// survive.
	labClickNav(t, tctx, "/settings")
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("wait settings: %v", err)
	}
	title, param, crumbs, _ = labRoute(t, tctx)
	if param != "" {
		t.Errorf("route.params.id after leaving the route = %q, want \"\" (previous route's param survived)", param)
	}
	if title != "Settings" || crumbs != "Home / Settings" {
		t.Errorf("after leaving, title/crumbs = (%q,%q), want (Settings,Home / Settings)", title, crumbs)
	}

	// Back replays the cached /inbox/2 snapshot INCLUDING its route.
	labBack(t, tctx)
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-MESSAGE-2`, chromedp.ByQuery),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("wait back: %v", err)
	}
	title, param, crumbs, mix = labRoute(t, tctx)
	if title != "Message 2" || param != "2" || crumbs != "Home / Inbox / 2" || mix != "/inbox/2|2" {
		t.Fatalf("after Back, route bindings = (%q,%q,%q,%q), want the /inbox/2 snapshot", title, param, crumbs, mix)
	}

	// Cross-chain swap: /bare owns its own layout, so the navigation
	// leaves the shell chain (full-document swap). The fetched head's
	// route keys merge through the same atomic path, so the store
	// follows the swap and the recorder still sees no mix.
	if err := chromedp.Run(tctx, page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/bare')`, nil),
	); err != nil {
		t.Fatalf("navigate /bare: %v", err)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-BARE`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("wait bare: %v", err)
	}
	title, param, crumbs, mix = labRoute(t, tctx)
	if title != "Bare" || param != "" || crumbs != "Home / Bare" || mix != "/bare|" {
		t.Fatalf("after cross-chain, route bindings = (%q,%q,%q,%q), want (Bare,'',Home / Bare,/bare|)", title, param, crumbs, mix)
	}
	var rec2 []string
	if err := chromedp.Run(tctx, chromedp.Evaluate(`JSON.stringify(window.__mixRec || [])`, &title)); err != nil {
		t.Fatalf("read recorder 2: %v", err)
	}
	if err := json.Unmarshal([]byte(title), &rec2); err != nil {
		t.Fatalf("recorder2 json: %v", err)
	}
	mixed2 := 0
	for _, e := range rec2 {
		parts := strings.SplitN(e, "|", 2)
		if len(parts) == 2 && parts[1] != "" && !strings.HasSuffix(parts[0], "/"+parts[1]) {
			mixed2++
		}
	}
	t.Logf("P7-C cross-chain: recorder holds %d evaluations total, %d mixed: %v", len(rec2), mixed2, rec2)
	if mixed2 != 0 {
		t.Errorf("%d mixed evaluations across the cross-chain swap; the atomic merge covers it too", mixed2)
	}

	// First-declaration render: a FRESH tab full-loading /first/abc is
	// the first render in this document that declares route.params.tag,
	// and it carries the value — under P7-B it stamped ''.
	fresh, fcancel := chromedp.NewContext(browser)
	t.Cleanup(fcancel)
	if err := chromedp.Run(fresh,
		chromedp.Navigate(base+"/first/abc"),
		chromedp.WaitVisible(`#lab-SCREEN-FIRST-abc`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate /first/abc: %v", err)
	}
	var tag string
	if err := chromedp.Run(fresh, chromedp.Evaluate(`document.getElementById('lab-first-tag').textContent`, &tag)); err != nil {
		t.Fatalf("read first tag: %v", err)
	}
	if tag != "abc" {
		t.Fatalf("first-declaration render stamped route.params.tag = %q, want \"abc\" (the raw bag write must cover a slice declared by this very render)", tag)
	}

	// Hostile values: markup in a param (and so in the dynamic title)
	// renders as text and never executes.
	if err := chromedp.Run(tctx, page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/inbox/<img src=x onerror=window.__labPwned=1>')`, nil),
	); err != nil {
		t.Fatalf("navigate hostile: %v", err)
	}
	if err := chromedp.Run(tctx,
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => JSON.stringify({
			title: document.getElementById('lab-route-title').textContent,
			param: document.getElementById('lab-route-param').textContent,
			imgs: document.querySelectorAll('#shell-header img').length,
			pwned: window.__labPwned === undefined,
		}))()`, &title),
	); err != nil {
		t.Fatalf("hostile probe: %v", err)
	}
	var hostile struct {
		Title string `json:"title"`
		Param string `json:"param"`
		Imgs  int    `json:"imgs"`
		Pwned bool   `json:"pwned"`
	}
	if err := json.Unmarshal([]byte(title), &hostile); err != nil {
		t.Fatalf("hostile json: %v", err)
	}
	if hostile.Imgs != 0 || !hostile.Pwned {
		t.Fatalf("hostile param executed or rendered as markup: imgs=%d pwnedSafe=%v", hostile.Imgs, hostile.Pwned)
	}
	if !strings.Contains(hostile.Title, "Message") || hostile.Param == "" {
		t.Errorf("hostile values did not render as text: title=%q param=%q", hostile.Title, hostile.Param)
	}
}

// labListState samples everything client-side the list pane and the
// toolbar carry (P8): filter text, counter display, a row's element
// identity probe, window scroll, and the toolbar input's text.
func labListState(t *testing.T, ctx context.Context) (filter, count, rowProbe, tool string, scrollY float64) {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => JSON.stringify({
		f: (document.getElementById('lab-filter') || {value: '!missing'}).value,
		c: (document.getElementById('lab-count') || {textContent: '!missing'}).textContent.trim(),
		r: (document.querySelector('[data-row="100"]') || {}).__labRow || 'gone',
		t: (document.getElementById('lab-tool-input') || {value: '!missing'}).value,
		s: window.scrollY,
	}))()`, &out)); err != nil {
		t.Fatalf("list state: %v", err)
	}
	var v struct {
		F string  `json:"f"`
		C string  `json:"c"`
		R string  `json:"r"`
		T string  `json:"t"`
		S float64 `json:"s"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("list state json: %v", err)
	}
	return v.F, v.C, v.R, v.T, v.S
}

// TestListDetailNestedLayerE2E pins P8 variant B: NO hashes. The list
// pane is a static part of the nested /items layout layer, so the
// layer chain keeps it across /items/:id navigation and Back — and
// across refresh() too (the swap boundary is the inner layer's
// primary; the kept layer sits above it). The identical TOOLBAR fill
// is re-applied on every navigation, so its input's text is LOST (the
// recorded cost of B). Window scroll is reset by the runtime's
// scroll-to-top on every click navigation (same under both variants;
// scroll-per-outlet is deferred until P8 settles).
func TestListDetailNestedLayerE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	waitDetail := func(id string) error {
		return chromedp.Run(tctx,
			chromedp.WaitVisible(`#lab-SCREEN-DETAIL-`+id, chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond),
		)
	}

	// Full load /items/1: list present with 200 rows.
	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/items/1"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var rows int
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(`document.querySelectorAll('#lab-list a[data-row]').length`, &rows),
	); err != nil || rows != 200 {
		t.Fatalf("list rows = %d (err %v), want 200", rows, err)
	}
	// Stamp identity, scroll ListDetail's list container, type, and bump.
	// The pane's scroll position must survive detail navigation.
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(`document.querySelector('[data-row="100"]').__labRow = 'alive'; true`, nil),
	); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if err := chromedp.Run(tctx,
		chromedp.SendKeys(`#lab-filter`, "hello"),
		chromedp.Click(`#lab-count-btn`, chromedp.ByID),
		chromedp.Click(`#lab-count-btn`, chromedp.ByID),
		chromedp.SendKeys(`#lab-tool-input`, "xx"),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("interact: %v", err)
	}
	f, c, r, tl, _ := labListState(t, tctx)
	if f != "hello" || c != "2" || tl != "xx" {
		t.Fatalf("pre-nav state = (%q,%q,%q), want (hello,2,xx)", f, c, tl)
	}

	// Scroll the pane LAST: SendKeys on the filter focuses it, and the
	// focus scrolls the pane to the filter (top); the recorded state
	// must be what the user ends on, not an earlier intermediate.
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(`document.querySelector('nav:has(> #lab-list)').scrollTop = 400; true`, nil),
		chromedp.Sleep(100*time.Millisecond),
	); err != nil {
		t.Fatalf("scroll pane: %v", err)
	}

	// /items/1 -> /items/2 (row link): the nested items LAYER is kept.
	// Programmatic click: chromedp's coordinate click first scrolls
	// row 2 into view INSIDE the pane, which would move the pane before
	// the navigation and make the pane-survival read below vacuous.
	labSkipVT(t, tctx)
	if err := chromedp.Run(tctx, page.BringToFront(), chromedp.Evaluate(
		`document.querySelector('a[data-row="2"]').click(); true`, nil)); err != nil {
		t.Fatalf("click row 2: %v", err)
	}
	if err := waitDetail("2"); err != nil {
		t.Fatalf("wait detail 2: %v", err)
	}
	f, c, r, tl, sy := labListState(t, tctx)
	if f != "hello" || c != "2" || r != "alive" {
		t.Errorf("after /items/2: list state = (%q,%q,%q), want (hello,2,alive) — the kept layer must keep the list DOM", f, c, r)
	}
	t.Logf("after /items/2: toolbar input = %q (B re-applies the IDENTICAL toolbar fill; the typed text is lost — variant A skipped it and kept \"xx\")", tl)
	if pane := labPaneScroll(t, tctx); pane < 390 || pane > 410 {
		t.Errorf("after /items/2: pane scrollTop = %v, want ~400 (the kept layer keeps the pane DOM)", pane)
	}
	t.Logf("after /items/2: window scrollY %v (the /items pages no longer scroll the window since the pane is bounded; see P12)", sy)

	// -> /items/3: still kept.
	labSkipVT(t, tctx)
	if err := chromedp.Run(tctx, page.BringToFront(), chromedp.Evaluate(
		`document.querySelector('a[data-row="3"]').click(); true`, nil)); err != nil {
		t.Fatalf("click row 3: %v", err)
	}
	if err := waitDetail("3"); err != nil {
		t.Fatalf("wait detail 3: %v", err)
	}
	if f, c, _, _, _ = labListState(t, tctx); f != "hello" || c != "2" {
		t.Errorf("after /items/3: list state = (%q,%q), want (hello,2)", f, c)
	}

	// Back -> /items/2: the cached entry replays below the kept layer.
	labBack(t, tctx)
	if err := waitDetail("2"); err != nil {
		t.Fatalf("wait back to detail 2: %v", err)
	}
	if f, c, r, tl, _ = labListState(t, tctx); f != "hello" || c != "2" || r != "alive" {
		t.Errorf("after Back: list state = (%q,%q,%q), want (hello,2,alive)", f, c, r)
	}

	// refresh(): the swap boundary is still the inner layer's primary,
	// so the kept list layer SURVIVES the refresh (variant A re-applied
	// every fill and reset it). The toolbar fill re-applies as always.
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.refresh()`, nil),
	); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := waitDetail("2"); err != nil {
		t.Fatalf("wait refresh detail 2: %v", err)
	}
	f, c, r, tl, _ = labListState(t, tctx)
	if f != "hello" || c != "2" || r != "alive" {
		t.Errorf("after refresh(): list state = (%q,%q,%q), want (hello,2,alive) — the kept layer must survive a refresh", f, c, r)
	}
	if tl != "" {
		t.Errorf("after refresh(): toolbar input = %q, want re-applied empty", tl)
	}
}
