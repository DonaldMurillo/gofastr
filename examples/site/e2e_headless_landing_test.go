package main

// Browser-level (chromedp) proofs for /examples/headless/{default,dense}/landing,
// the theme-layer showcase of PR A3. One test per fixture, per the design
// (DESIGN-foundation.md "The showcase") and astra's §6 demands: theme
// variables computed per scope, nesting A → B → A, the option-only twin
// keeping the palette, scoped dark mode following the document, bare
// headless staying unstyled, the cold LoadAuto sheet, and the newsletter's
// two round trips (island with the runtime, native POST without it).
//
// The SSR-level checks at the top run without Chrome: route + wrapper
// class + 404 + StaticPaths are deterministic facts of the rendered HTML.

import (
	"fmt"
	"strings"
	"testing"

	cdplog "github.com/chromedp/cdproto/log"
	cdnetwork "github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	hlComfortableH = "44px" // --spacing-touch-target at comfortable density
	hlComfortableR = "6px"  // the site theme's --radii-md
	hlCompactH     = "36px" // compact density control height
	hlSquareR      = "0px"  // square radius
)

// hlForceLight forces the explicit light scheme before a test reads LIGHT
// token values: headless Chrome can arrive with a dark OS preference, and
// the scoped dark blocks would answer instead.
const hlForceLight = `document.documentElement.setAttribute('data-color-scheme','light')`

// hlPrimaryMetrics reads, from one element, the computed border radius,
// min-height (the density rule consumes --fui-density-control-h), and the
// two option variables themselves.
const hlPrimaryMetrics = `(() => {
  const el = document.querySelector(%q);
  const cs = getComputedStyle(el);
  return {
    radius: cs.borderRadius,
    minH: cs.minHeight,
    density: cs.getPropertyValue('--fui-density-control-h').trim(),
    buttonRadius: cs.getPropertyValue('--fui-button-radius').trim(),
    primary: cs.getPropertyValue('--color-primary').trim(),
  };
})()`

// TestLandingRoutesOtherIsARoute: every route's Other is some route's
// theme, so the nesting fixture's B label (landingOtherName) is never
// empty and the A → B → A walk stays inside the showcase.
func TestLandingRoutesOtherIsARoute(t *testing.T) {
	for _, r := range landingRoutes {
		if got := landingOtherName(r); got == "" || got == r.Name {
			t.Errorf("%s: Other resolves to %q, want another route's name", r.Segment, got)
		}
	}
}

func TestHeadlessLandingRoutesRender(t *testing.T) {
	for _, r := range landingRoutes {
		page := body(t, landingRoutePath(r.Segment))
		if !strings.Contains(page, ">Headless landing") {
			t.Errorf("%s: page title missing from render", r.Segment)
		}
		if !strings.Contains(page, r.Ref.Class()) {
			t.Errorf("%s route: the theme's wrapper class is not on the page", r.Segment)
		}
	}
	def := body(t, landingRoutePath("default"))
	// The two original themes must hash apart. (The other themes'
	// classes are legitimately on each page too: the nesting fixture
	// wraps them.)
	if landingRefDense.Class() == landingRefFramework.Class() {
		t.Error("the two route themes share one wrapper class; they must hash apart")
	}
	// An unknown theme segment is a 404, not a panic and not a wrong theme.
	for _, page := range []string{"landing", "dashboard"} {
		if got := serve(t, "GET", "/examples/headless/retro/"+page).Code; got != 404 {
			t.Errorf("unknown theme segment on the %s = %d, want 404", page, got)
		}
	}
	paths := (&HeadlessLandingScreen{}).StaticPaths(t.Context())
	if len(paths) != len(landingRoutes) {
		t.Errorf("StaticPaths = %v, want one entry per registered theme (%d)", paths, len(landingRoutes))
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p["theme"]] {
			t.Errorf("StaticPaths repeats theme %q", p["theme"])
		}
		seen[p["theme"]] = true
	}
	for _, r := range landingRoutes {
		if !seen[r.Segment] {
			t.Errorf("StaticPaths misses registered theme %q", r.Segment)
		}
	}
	// The Strings bridge on the bare fixture: the site installs no
	// translator, so ui.StringsFor(r.Context()) must leave the page
	// saying headless's own English — the tone word before the
	// banner's title and the dismiss control's formatted name. A
	// translated word here would mean the bridge stopped falling
	// back to the English defaults.
	for _, want := range []string{
		"Information: ",                          // ToneInfo, said before the title
		"Dismiss: Strings from the request",      // DismissTitled, formatted with the title
		"The tone word and the dismiss name are", // the banner's own text
	} {
		if !strings.Contains(def, want) {
			t.Errorf("bare fixture: %q missing from the page — the Strings bridge changed the no-translator English", want)
		}
	}
}

func TestE2E_HeadlessLanding_ThemeVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)

	type metrics struct {
		Radius, MinH, Density, ButtonRadius, Primary string
	}
	for _, tc := range []struct {
		seg, wantClass                      string
		wantH, wantR, wantButtonR, wantPrim string
	}{
		{"default", landingRefFramework.Class(), hlComfortableH, hlComfortableR, hlComfortableR, "oklch(0.82 0.155 78)"},
		{"dense", landingRefDense.Class(), hlCompactH, hlSquareR, "0", "#0F766E"},
	} {
		t.Run(tc.seg, func(t *testing.T) {
			ctx := newE2EBrowserCtx(t)
			var m metrics
			var hasWrapper bool
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+landingRoutePath(tc.seg)),
				pageReady(),
				chromedp.Evaluate(hlForceLight, nil),
				chromedp.Evaluate(`!!document.querySelector('main .`+tc.wantClass+`')`, &hasWrapper),
				chromedp.Evaluate(fmtHL(hlPrimaryMetrics, "#hl-variant-primary"), &m),
			)
			if err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if !hasWrapper {
				t.Fatalf("theme wrapper .%s absent from the page", tc.wantClass)
			}
			if m.MinH != tc.wantH {
				t.Errorf("%s: primary button min-height = %q, want %q (the density variable the route's theme declares)", tc.seg, m.MinH, tc.wantH)
			}
			if m.Radius != tc.wantR {
				t.Errorf("%s: primary button border-radius = %q, want %q", tc.seg, m.Radius, tc.wantR)
			}
			if m.Density != tc.wantH {
				t.Errorf("%s: --fui-density-control-h = %q, want %q", tc.seg, m.Density, tc.wantH)
			}
			if m.ButtonRadius != tc.wantButtonR {
				t.Errorf("%s: --fui-button-radius = %q, want %q", tc.seg, m.ButtonRadius, tc.wantButtonR)
			}
			if !strings.EqualFold(m.Primary, tc.wantPrim) {
				t.Errorf("%s: --color-primary = %q, want %q", tc.seg, m.Primary, tc.wantPrim)
			}
		})
	}
}

// TestE2E_HeadlessLanding_WrapperClassesDiffer pins that the first two routes
// really render under two different theme scopes (the class is the scope).
func TestE2E_HeadlessLanding_WrapperClassesDiffer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var defClass, denseClass string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+landingRoutePath("default")),
		pageReady(),
		chromedp.Evaluate(`(document.querySelector('main div[class^="fui-theme-"]')||{}).className||''`, &defClass),
		chromedp.Navigate(base+landingRoutePath("dense")),
		pageReady(),
		chromedp.Evaluate(`(document.querySelector('main div[class^="fui-theme-"]')||{}).className||''`, &denseClass),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if defClass == "" || denseClass == "" || defClass == denseClass {
		t.Fatalf("wrapper classes must differ per route, got %q vs %q", defClass, denseClass)
	}
	if defClass != landingRefFramework.Class() || denseClass != landingRefDense.Class() {
		t.Fatalf("wrapper classes drifted from the registered refs: %q / %q", defClass, denseClass)
	}
}

func TestE2E_HeadlessLanding_NestingABA(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)

	type metrics struct {
		Radius, MinH string
	}
	for _, tc := range []struct {
		seg            string
		aH, aR, bH, bR string
	}{
		{"default", hlComfortableH, hlComfortableR, hlCompactH, hlSquareR},
		{"dense", hlCompactH, hlSquareR, hlComfortableH, hlComfortableR},
	} {
		t.Run(tc.seg, func(t *testing.T) {
			ctx := newE2EBrowserCtx(t)
			var all []metrics
			err := chromedp.Run(ctx,
				chromedp.Navigate(base+landingRoutePath(tc.seg)),
				pageReady(),
				chromedp.Evaluate(`(() => {
  const btns = document.querySelectorAll('#hl-nesting .fui-button--primary');
  return Array.from(btns).map(b => {
    const cs = getComputedStyle(b);
    return {radius: cs.borderRadius, minH: cs.minHeight};
  });
})()`, &all),
			)
			if err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if len(all) != 3 {
				t.Fatalf("#hl-nesting has %d primary buttons, want 3 (A → B → A)", len(all))
			}
			for i, want := range [][2]string{{tc.aH, tc.aR}, {tc.bH, tc.bR}, {tc.aH, tc.aR}} {
				if all[i].MinH != want[0] || all[i].Radius != want[1] {
					t.Errorf("level %d = %s/%s, want %s/%s — the innermost A must reset to the page's values", i, all[i].MinH, all[i].Radius, want[0], want[1])
				}
			}
		})
	}
}

func TestE2E_HeadlessLanding_OptionsTwinKeepsPalette(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	type m struct{ Radius, MinH string }
	var got struct {
		PagePrim, TwinPrim string
		PageM, TwinM       m
	}
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+landingRoutePath("default")),
		pageReady(),
		chromedp.Evaluate(`(() => {
  const sec = document.getElementById('hl-options');
  const twin = sec.querySelector('div[class^="fui-theme-"]');
  const ps = getComputedStyle(sec.querySelector('.fui-button--primary'));
  const ts = getComputedStyle(twin.querySelector('.fui-button--primary'));
  return {
    pagePrim: ps.getPropertyValue('--color-primary').trim(),
    twinPrim: ts.getPropertyValue('--color-primary').trim(),
    pageM: {radius: ps.borderRadius, minH: ps.minHeight},
    twinM: {radius: ts.borderRadius, minH: ts.minHeight},
  };
})()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	pagePrim, twinPrim, pageM, twinM := got.PagePrim, got.TwinPrim, got.PageM, got.TwinM

	if pagePrim != twinPrim || pagePrim == "" {
		t.Errorf("--color-primary: page = %q, twin = %q — the twin must keep the page's palette", pagePrim, twinPrim)
	}
	if twinM.Radius == pageM.Radius || twinM.MinH == pageM.MinH {
		t.Errorf("twin did not flip the options: page %s/%s vs twin %s/%s — radius and density must differ", pageM.MinH, pageM.Radius, twinM.MinH, twinM.Radius)
	}
	if twinM.MinH != hlCompactH || twinM.Radius != hlSquareR {
		t.Errorf("twin = %s/%s, want %s/%s (compact, square)", twinM.MinH, twinM.Radius, hlCompactH, hlSquareR)
	}
}

func TestE2E_HeadlessLanding_ScopedDarkModeFollowsDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var scopedLight, scopedDark, rootDark string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+landingRoutePath("dense")),
		pageReady(),
		// Force explicit light first (headless Chrome can arrive with a
		// dark OS preference): the scoped LIGHT block must answer.
		chromedp.Evaluate(hlForceLight, nil),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('main .`+landingRefDense.Class()+`')).getPropertyValue('--color-background').trim()`, &scopedLight),
		chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme','dark')`, nil),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('main .`+landingRefDense.Class()+`')).getPropertyValue('--color-background').trim()`, &scopedDark),
		chromedp.Evaluate(`getComputedStyle(document.body).getPropertyValue('--color-background').trim()`, &rootDark),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if scopedDark == scopedLight {
		t.Errorf("scoped --color-background did not flip with the document scheme: %q both ways — the scoped dark block is not being applied", scopedDark)
	}
	if scopedDark == rootDark {
		t.Errorf("scoped dark background %q equals the root's %q — the read is not observing the scope's own dark palette", scopedDark, rootDark)
	}
	if !strings.Contains(strings.ToLower(scopedDark), "#0c1a19") {
		t.Errorf("scoped dark background = %q, want the dense theme's #0C1A19", scopedDark)
	}
}

func TestE2E_HeadlessLanding_BareHeadlessStaysUnstyled(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var bareClass string
	var bareBG, styledBG string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+landingRoutePath("default")),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('hl-bare-button').className`, &bareClass),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('hl-bare-button')).backgroundColor`, &bareBG),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('hl-styled-button')).backgroundColor`, &styledBG),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if strings.Contains(bareClass, "fui-") {
		t.Errorf("bare headless button carries a fui- class %q; nil Classes must render none", bareClass)
	}
	if bareBG == styledBG {
		t.Errorf("bare (%s) and styled (%s) buttons compute the same background — the bare one is being styled", bareBG, styledBG)
	}
}

func TestE2E_HeadlessLanding_ColdLoadAutoSheet(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	var absentBefore, presentAfter, fragmentAfter bool
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+landingRoutePath("default")),
		pageReady(),
		// Before: the LoadAuto sheet is not on the page and the region
		// holds its initial copy.
		chromedp.Evaluate(`document.querySelector('link[data-fui-style="ui-callout"]') === null`, &absentBefore),
		// Click: the button fetches the fragment, the signal region
		// swaps, and the runtime must scan the insertion for
		// data-fui-comp and fetch the sheet. Condition waits, not a
		// fixed sleep: the fragment arriving and the sheet landing are
		// the two facts under test.
		chromedp.Click(`#hl-late-button`, chromedp.ByQuery),
		waitModule(`!!document.getElementById('hl-late-fragment')`),
		waitModule(`!!document.querySelector('link[data-fui-style="ui-callout"]')`),
		chromedp.Evaluate(`!!document.getElementById('hl-late-fragment')`, &fragmentAfter),
		chromedp.Evaluate(`!!document.querySelector('link[data-fui-style="ui-callout"]')`, &presentAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !absentBefore {
		t.Error("the ui-callout sheet was already on the page before the click — the fixture is not cold")
	}
	if !fragmentAfter {
		t.Fatal("the late fragment never arrived in the signal region")
	}
	if !presentAfter {
		t.Error("no <link data-fui-style=\"ui-callout\"> after the insertion — demand loading of the LoadAuto sheet did not fire")
	}
	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("cold load produced console/CSP errors:\n  %s", strings.Join(errs, "\n  "))
	}
}

func TestE2E_HeadlessLanding_NewsletterIslandRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	var focused string
	var summaryShown, doneShown bool
	err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		chromedp.Navigate(base+landingRoutePath("default")),
		pageReady(),
		// Empty submit: the server rejects, answers 200 with the
		// re-rendered region, the island swaps it in, and the headless
		// behaviour module moves focus to the summary.
		chromedp.Click(`#hl-newsletter button[type="submit"]`, chromedp.ByQuery),
		waitModule(`!!document.getElementById('hl-subscribe-errors')`),
		chromedp.Evaluate(`String(document.activeElement === document.getElementById('hl-subscribe-errors'))`, &focused),
		// Valid submit: the same round trip renders the success callout.
		chromedp.SetValue(`#hl-subscribe-email`, "reader@example.com", chromedp.ByQuery),
		chromedp.Click(`#hl-newsletter button[type="submit"]`, chromedp.ByQuery),
		waitModule(`!!document.getElementById('hl-subscribe-done')`),
		chromedp.Evaluate(`!!document.getElementById('hl-subscribe-done')`, &doneShown),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if focused != "true" {
		t.Error("focus did not land on the error summary after the failed island submit")
	}
	_ = summaryShown
	if !doneShown {
		t.Error("the success callout never rendered after the valid submit")
	}
	// A 200 round trip should log nothing; any entry for the endpoint is
	// tolerated the way the optimistic reject paths are, everything else
	// stays fatal.
	if errs := sink.errorsExcludingExpectedReject("/__site/headless/subscribe"); len(errs) > 0 {
		t.Errorf("newsletter island produced %d console/CSP/network error(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
}
func TestE2E_HeadlessLanding_NewsletterNoScriptRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var afterInvalid, afterValid string
	var summaryShown, doneShown bool
	err := chromedp.Run(ctx,
		// No-script pass: block the runtime (and its split modules) the
		// way a reader with script disabled experiences the page. The
		// form still POSTs natively; the handler answers 303 back to
		// the landing route and the page renders the region from the
		// query it lands with.
		cdnetwork.Enable(),
		cdnetwork.SetBlockedURLs().WithURLPatterns([]*cdnetwork.BlockPattern{
			{URLPattern: "*://*:*/*runtime.js*", Block: true},
			{URLPattern: "*://*:*/*__gofastr/runtime/*", Block: true},
		}),
		chromedp.Navigate(base+landingRoutePath("default")),
		pageReady(),
		// Invalid submit with a typed address: the 303 lands on the
		// landing route carrying the outcome alone — the address never
		// travels in the URL — and the error summary renders.
		chromedp.SetValue(`#hl-subscribe-email`, "not-an-address", chromedp.ByQuery),
		chromedp.Click(`#hl-newsletter button[type="submit"]`, chromedp.ByQuery),
		// WaitVisible, not Poll: the submit navigates (native POST →
		// 303 → GET), and a poll task does not survive navigation
		// (chromedp raises "Inspected target navigated or closed"),
		// while the query actions retry onto the new document.
		chromedp.WaitVisible(`#hl-subscribe-errors`, chromedp.ByID),
		chromedp.Location(&afterInvalid),
		chromedp.Evaluate(`!!document.getElementById('hl-subscribe-errors')`, &summaryShown),
		// Valid resubmit from the answered page: the 303 carries
		// subscribe=ok and the success callout renders.
		chromedp.SetValue(`#hl-subscribe-email`, "reader@example.com", chromedp.ByQuery),
		chromedp.Click(`#hl-newsletter button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#hl-subscribe-done`, chromedp.ByID),
		chromedp.Location(&afterValid),
		chromedp.Evaluate(`!!document.getElementById('hl-subscribe-done')`, &doneShown),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The proof the runtime never intercepted the submit is the URL: a
	// native form POST navigates to the handler's 303 target — the
	// landing route with the answer in its query — where an island
	// round trip would have stayed put. (window.__gofastr is no probe
	// here: an inline bootstrap stub can define the name.)
	invalidURL := base + landingRoutePath("default") + "?"
	if !strings.HasPrefix(afterInvalid, invalidURL) || !strings.Contains(afterInvalid, "subscribe=invalid") {
		t.Fatalf("after the invalid submit the browser is at %q, want %s…subscribe=invalid — without the runtime the POST must navigate and the 303 must land back on the landing route", afterInvalid, invalidURL)
	}
	// The submitted address must not ride in the URL: it would sit in
	// history and in any referrer a later click sends.
	if strings.Contains(afterInvalid, "not-an-address") {
		t.Errorf("the landing URL %q carries the submitted address", afterInvalid)
	}
	if !strings.HasPrefix(afterValid, invalidURL) || !strings.Contains(afterValid, "subscribe=ok") {
		t.Fatalf("after the valid submit the browser is at %q, want %s…subscribe=ok", afterValid, invalidURL)
	}
	if !summaryShown {
		t.Error("no-script submit: the error summary never rendered in the answered page")
	}
	if !doneShown {
		t.Error("no-script valid submit: the success callout never rendered")
	}
}

// fmtHL fills a %q selector into the metrics probe.
func fmtHL(format, selector string) string {
	return strings.ReplaceAll(format, "%q", "`"+selector+"`")
}

// TestE2E_HeadlessLanding_FieldLayoutsUnderConstraint is the
// FieldOptions acceptance pass: the long label wraps rather than
// widening its track, both messages render at once with the error
// first, the choice row sits beside the ordinary fields, and nothing
// overflows — in a narrow grid CELL at a wide viewport (the
// container case) as well as at a narrow viewport.
func TestE2E_HeadlessLanding_FieldLayoutsUnderConstraint(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	for _, route := range landingRoutes {
		for _, vp := range []struct{ w, h int64 }{{1280, 800}, {390, 844}} {
			t.Run(fmt.Sprintf("%s/%dx%d", route.Segment, vp.w, vp.h), func(t *testing.T) {
				ctx := newE2EBrowserCtx(t)
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(vp.w, vp.h),
					chromedp.Navigate(base+landingRoutePath(route.Segment)),
					pageReady(),
					chromedp.WaitVisible(`#hl-field-layout-section`, chromedp.ByID),
				); err != nil {
					t.Fatalf("chromedp: %v", err)
				}
				var got struct {
					Overflow        bool
					HintVisible     bool
					ErrorVisible    bool
					ErrorFirst      bool
					LongWraps       bool
					CheckboxVisible bool
					CellOverflow    bool
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
					const de = document.documentElement;
					let cellOverflow = false;
					for (const f of document.querySelectorAll('#hl-field-layout-section .fui-field')) {
						if (f.scrollWidth > f.clientWidth + 1) cellOverflow = true;
					}
					// The ids sit on the CONTROLS; the field roots are
					// their closest .fui-field.
					const fieldOf = (id) => document.getElementById(id).closest('.fui-field');
					const both = fieldOf('hl-field-both');
					const hint = both.querySelector('.fui-field__hint');
					const err = both.querySelector('.fui-field__error');
					const longLabel = fieldOf('hl-field-long').querySelector('.fui-field__label');
					const bothLabel = both.querySelector('.fui-field__label');
					const cb = document.getElementById('hl-field-checkbox');
					return {
						overflow: de.scrollWidth > window.innerWidth + 1,
						hintVisible: !!hint && hint.offsetParent !== null,
						errorVisible: !!err && err.offsetParent !== null,
						errorFirst: !!hint && !!err && (err.compareDocumentPosition(hint) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0,
						longWraps: !!longLabel && !!bothLabel && longLabel.offsetHeight > bothLabel.offsetHeight + 4,
						checkboxVisible: !!cb && cb.offsetParent !== null,
						cellOverflow,
					};
				})()`, &got)); err != nil {
					t.Fatalf("probe: %v", err)
				}
				if got.Overflow {
					t.Error("the page overflows horizontally at this size")
				}
				if got.CellOverflow {
					t.Error("a field overflows its grid cell (the narrow-container case)")
				}
				if !got.HintVisible || !got.ErrorVisible {
					t.Errorf("both messages must be visible at once: hint=%v error=%v", got.HintVisible, got.ErrorVisible)
				}
				if !got.ErrorFirst {
					t.Error("the error must be drawn before the hint")
				}
				if !got.LongWraps {
					t.Error("the long label did not wrap to more lines than the short one")
				}
				if !got.CheckboxVisible {
					t.Error("the checkbox row is not visible beside the ordinary fields")
				}
			})
		}
	}
}
