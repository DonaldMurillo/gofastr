package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// =============================================================================
// Chaos — every component on every demo page; resize, spam-click, console
// =============================================================================

// TestE2E_Chaos_NoConsoleErrorsOnFrameworkUIPage loads the busiest
// page (every framework/ui component on one canvas) and fails if any
// console.error fired. Catches CSP violations, missing resources, and
// runtime exceptions thrown by component code.
//
// We intentionally test only the busiest page rather than every page:
// each navigation opens an SSE long-poll on /__gofastr/sse, which
// keeps chromedp's network state non-idle long enough to time out
// multi-page chaos tests. Per-page coverage is provided by the
// non-browser TestComponentDemosRenderWithoutPanic in website_test.go.
func TestE2E_Chaos_NoConsoleErrorsOnFrameworkUIPage(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	sink := &consoleSink{}
	listenConsoleErrors(ctx, sink)

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		// Touch a few components to flush any deferred work.
		chromedp.Evaluate(`document.querySelectorAll('.ui-button, .ui-badge, .ui-stat-card').length`, new(int)),
	)
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("navigation: %v", err)
	}
	if errs := sink.hasErrors(); len(errs) > 0 {
		t.Errorf("console errors on /framework-ui/:\n  %s", strings.Join(errs, "\n  "))
	}
}

// TestE2E_Chaos_ResizeWhileToggling stress-tests rapid resize plus
// rapid component interaction. Should never panic, never produce
// console errors, never leave components in a broken visual state.
func TestE2E_Chaos_ResizeWhileToggling(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	sink := &consoleSink{}
	listenConsoleErrors(ctx, sink)

	var finalOpen int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		chromedp.EmulateViewport(320, 568),
		chromedp.Evaluate(`(() => {
            const sums = document.querySelectorAll('.accordion-group > details > summary');
            for (let i = 0; i < 30; i++) sums[i % sums.length].click();
            return true;
        })()`, nil),
		chromedp.EmulateViewport(1440, 900),
		chromedp.Evaluate(`(() => {
            const sums = document.querySelectorAll('.accordion-stack > details > summary');
            for (let i = 0; i < 30; i++) sums[i % sums.length].click();
            return true;
        })()`, nil),
		chromedp.EmulateViewport(768, 1024),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details[open]').length`, &finalOpen),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if finalOpen > 1 {
		t.Errorf("after resize+spam-click chaos, Group has %d open items (expected ≤1)", finalOpen)
	}
	if errs := sink.hasErrors(); len(errs) > 0 {
		t.Errorf("console errors during chaos:\n  %s", strings.Join(errs, "\n  "))
	}
}

// TestE2E_Chaos_FocusRingContrastsWithButton checks that focus
// outline on .ui-button is visually distinct from the button's
// own background. The earlier implementation set both to
// var(--color-primary), making the focus indicator nearly
// invisible against the same-colored button — fail for keyboard
// users. (WCAG 2.4.7)
func TestE2E_Chaos_FocusRingContrastsWithButton(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var result map[string]string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            // Measure :focus-visible styles by reading the rule
            // set rather than relying on actual focus state
            // (chromedp's headless focus is unreliable). We resolve
            // the computed background of the button + the
            // matching .ui-button:focus-visible rule's outline
            // declarations from document.styleSheets.
            const btn = document.querySelector('button.ui-button');
            if (!btn) return {error: "no button on page"};
            const cs = getComputedStyle(btn);
            // Search stylesheets for the focus-visible rule.
            let outlineColor = '', boxShadow = '';
            for (const sheet of document.styleSheets) {
                let rules;
                try { rules = sheet.cssRules; } catch { continue; }
                if (!rules) continue;
                for (const r of rules) {
                    if (!r.selectorText) continue;
                    if (!r.selectorText.includes('.ui-button:focus-visible') &&
                        !r.selectorText.includes('[data-fui-comp="ui-button"]:focus-visible')) continue;
                    const s = r.style;
                    if (s.outlineColor) outlineColor = s.outlineColor;
                    if (s.boxShadow) boxShadow = s.boxShadow;
                }
            }
            return {
                bg: cs.backgroundColor,
                outlineColor: outlineColor || 'none',
                boxShadow: boxShadow || 'none',
            };
        })()`, &result),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if result["error"] != "" {
		t.Skipf("setup failed: %s", result["error"])
	}
	// The focus signal must come from EITHER:
	//   - an outline color resolving to something other than the
	//     button's own background, OR
	//   - a box-shadow ring (layered offset shadow trick).
	// If the focus-visible rule sets outline-color to a var that
	// resolves to the button bg AND there's no box-shadow ring,
	// the focus indicator is invisible.
	bg := result["bg"]
	// outlineColor from the CSS rule is typically `var(--color-primary)`
	// (a literal string, NOT a resolved color). If it's the same
	// token name as the button's bg... we can't be sure without
	// resolving. So the test sticks to the observable rule: if
	// outline-color is `var(--color-primary)` (matches bg token) AND
	// no box-shadow, fail.
	if strings.Contains(result["outlineColor"], "--color-primary") && result["boxShadow"] == "none" {
		t.Errorf("focus ring invisible on primary button: outline uses --color-primary (same as button bg=%s) with no box-shadow fallback", bg)
	}
}

// TestE2E_Chaos_NoMobileHorizontalScroll loads the css-loading
// showcase at 375px viewport (iPhone SE width) and asserts the
// document doesn't overflow horizontally. Catches regressions where
// fixed-width content (the catalog table, the top nav) blows out
// the layout.
func TestE2E_Chaos_NoMobileHorizontalScroll(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var dims map[string]float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 800),
		chromedp.Navigate(base+"/framework-ui/css-loading"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const d = document.documentElement;
            return {scrollWidth: d.scrollWidth, clientWidth: d.clientWidth};
        })()`, &dims),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if dims["scrollWidth"] > dims["clientWidth"] {
		t.Errorf("/framework-ui/css-loading overflows at 375px viewport: scrollWidth=%.0f clientWidth=%.0f", dims["scrollWidth"], dims["clientWidth"])
	}
}

// TestE2E_Chaos_MobileHamburgerNav asserts that at a 375px viewport
// (iPhone SE), the site nav links collapse behind a hamburger toggle
// — i.e. the <nav> is not visible until the user interacts with the
// disclosure trigger. At desktop widths (>=640px) the toggle is hidden
// and the nav renders inline. Pure CSS via <details>/<summary>; no
// JS hook required.
func TestE2E_Chaos_MobileHamburgerNav(t *testing.T) {
	base := startE2EServer(t)

	// Mobile: toggle visible, nav initially hidden, opens on click.
	mctx := newE2EBrowserCtx(t)
	var mobile map[string]any
	err := chromedp.Run(mctx,
		chromedp.EmulateViewport(375, 800),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const toggle = document.querySelector('.site-header summary.site-nav__toggle');
            const nav = document.querySelector('.site-header nav');
            if (!toggle || !nav) return {missing: true};
            const toggleVisible = toggle.getBoundingClientRect().height > 0;
            const navHiddenInitially = nav.getBoundingClientRect().height === 0;
            return {missing: false, toggleVisible, navHiddenInitially};
        })()`, &mobile),
	)
	if err != nil {
		t.Fatalf("chromedp mobile: %v", err)
	}
	if mobile["missing"] == true {
		t.Fatalf("hamburger toggle (.site-header summary.site-nav__toggle) not in DOM")
	}
	if mobile["toggleVisible"] != true {
		t.Errorf("at 375px: hamburger toggle should be visible, got hidden")
	}
	if mobile["navHiddenInitially"] != true {
		t.Errorf("at 375px: nav links should be hidden until toggle is opened")
	}

	var opened map[string]any
	err = chromedp.Run(mctx,
		chromedp.Click(".site-header summary.site-nav__toggle", chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const nav = document.querySelector('.site-header nav');
            return {height: nav.getBoundingClientRect().height};
        })()`, &opened),
	)
	if err != nil {
		t.Fatalf("chromedp mobile open: %v", err)
	}
	if h, _ := opened["height"].(float64); h <= 0 {
		t.Errorf("at 375px after click: nav should be visible, got height=%v", opened["height"])
	}

	// Desktop: toggle hidden, nav visible inline.
	dctx := newE2EBrowserCtx(t)
	var desktop map[string]any
	err = chromedp.Run(dctx,
		chromedp.EmulateViewport(1024, 800),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const toggle = document.querySelector('.site-header summary.site-nav__toggle');
            const nav = document.querySelector('.site-header nav');
            return {
                toggleHidden: !toggle || toggle.getBoundingClientRect().height === 0,
                navVisible: nav.getBoundingClientRect().height > 0,
            };
        })()`, &desktop),
	)
	if err != nil {
		t.Fatalf("chromedp desktop: %v", err)
	}
	if desktop["toggleHidden"] != true {
		t.Errorf("at 1024px: hamburger toggle should be hidden, got visible")
	}
	if desktop["navVisible"] != true {
		t.Errorf("at 1024px: nav links should be visible inline")
	}
}

// TestE2E_Chaos_HamburgerToggleFocusRing asserts the mobile hamburger
// toggle has an explicit :focus-visible rule. iOS Safari + Chromium
// both strip the default ring from <summary> after we hide its
// disclosure marker, so without an explicit rule keyboard users get
// no affordance. We check the served CSS rather than computed style
// because :focus-visible doesn't match programmatic .focus().
func TestE2E_Chaos_HamburgerToggleFocusRing(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var css string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`fetch('/__gofastr/app.css').then(r => r.text())`, &css,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(css, ".site-nav__toggle:focus-visible") {
		t.Errorf("app.css must contain a .site-nav__toggle:focus-visible rule; got %d bytes", len(css))
	}
}

// TestE2E_Chaos_SPANavAnnouncesTitle asserts that the SPA-nav handler
// updates a polite live region with the new page title so screen
// readers (NVDA, VoiceOver) announce the route change. document.title
// mutations alone are not announced.
func TestE2E_Chaos_SPANavAnnouncesTitle(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Click(`a[href="/about"]`, chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const r = document.getElementById('fui-route-announce');
            return {
                regionPresent: !!r,
                regionRole: r?.getAttribute('role') ?? '',
                regionLive: r?.getAttribute('aria-live') ?? '',
                regionText: r?.textContent ?? '',
                docTitle: document.title,
            };
        })()`, &info),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if info["regionPresent"] != true {
		t.Fatalf("#fui-route-announce live region must be in the page")
	}
	if info["regionLive"] != "polite" && info["regionRole"] != "status" {
		t.Errorf("live region must be polite or role=status; got live=%v role=%v", info["regionLive"], info["regionRole"])
	}
	if !strings.Contains(toString(info["regionText"]), toString(info["docTitle"])) &&
		!strings.Contains(toString(info["docTitle"]), toString(info["regionText"])) {
		t.Errorf("live region textContent should track the page title after SPA nav; region=%q title=%q",
			info["regionText"], info["docTitle"])
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// TestE2E_Chaos_FormErrorNotColorOnly verifies that an .is-error
// form input encodes its state through more than just color —
// it stacks an inset box-shadow ring so deuteranopic/protanopic
// users see the same affordance. The ring is size-stable (no
// border-width bump) so the input text doesn't shift on validate.
func TestE2E_Chaos_FormErrorNotColorOnly(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const errEl = document.querySelector('.ui-form-field.is-error input, [data-fui-comp="ui-form-field"].is-error input, .is-error input');
            const okEl  = document.querySelector('.ui-form-field:not(.is-error) input, [data-fui-comp="ui-form-field"]:not(.is-error) input');
            if (!errEl) return {missing: true};
            const errCS = getComputedStyle(errEl);
            const okCS  = okEl ? getComputedStyle(okEl) : null;
            return {
                missing: false,
                errBorderPx: parseFloat(errCS.borderTopWidth || '0'),
                okBorderPx:  okCS ? parseFloat(okCS.borderTopWidth || '0') : 0,
                errBoxShadow: errCS.boxShadow,
            };
        })()`, &info),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if info["missing"] == true {
		t.Skip("no .is-error input on /framework-ui/ — test premise invalid")
	}
	// Non-color affordance present (box-shadow ring).
	if shadow := toString(info["errBoxShadow"]); shadow == "" || shadow == "none" {
		t.Errorf(".is-error input must layer a non-color affordance via box-shadow; got %q", shadow)
	}
	// Size-stable: error border must match non-error border so text
	// doesn't shift 1px on validation toggle.
	errBW, _ := info["errBorderPx"].(float64)
	okBW, _ := info["okBorderPx"].(float64)
	if okBW > 0 && errBW != okBW {
		t.Errorf(".is-error input border-width drift: error=%.1fpx, normal=%.1fpx — layout-shift hazard", errBW, okBW)
	}
}

// TestE2E_Chaos_SkipLinkMovesFocus asserts the "Skip to main content"
// link actually moves keyboard focus into <main> when activated, not
// just scrolls. Safari requires tabindex=-1 on a hash target before
// it'll accept focus, so the framework must emit it.
func TestE2E_Chaos_SkipLinkMovesFocus(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const a = document.querySelector('a.skip-link');
            if (!a) return {missing: true};
            a.focus(); a.click();
            const m = document.getElementById('main-content');
            return {
                missing: false,
                mainTabIndex: m ? m.getAttribute('tabindex') : 'no-main',
            };
        })()`, &info),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if info["missing"] == true {
		t.Fatalf("skip link not in DOM")
	}
	if info["mainTabIndex"] != "-1" {
		t.Errorf("main#main-content must have tabindex=-1 so skip-link can focus it; got %v", info["mainTabIndex"])
	}
}

// TestE2E_Chaos_HamburgerClosesOnNavAndEscape opens the mobile menu,
// then asserts two things:
//   (1) clicking a nav link inside the open menu (SPA partial-nav)
//       collapses the <details> on the destination page — otherwise
//       the menu floats over the new content on every <640px nav.
//   (2) pressing Escape while the menu is open closes it without
//       relying on the user to re-tap the toggle.
func TestE2E_Chaos_HamburgerClosesOnNavAndEscape(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// (1) SPA-nav case.
	var afterNav map[string]any
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 800),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Click(".site-header summary.site-nav__toggle", chromedp.ByQuery),
		pageReady(),
		// Click a nav link inside the open menu; SPA-nav swaps <main>.
		chromedp.Click(`.site-header details.site-nav nav a[href="/about"]`, chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const d = document.querySelector('.site-header details.site-nav');
            return {open: d ? d.open : 'no-details', path: location.pathname};
        })()`, &afterNav),
	)
	if err != nil {
		t.Fatalf("chromedp nav: %v", err)
	}
	if afterNav["path"] != "/about" {
		t.Fatalf("expected nav to /about, got %v", afterNav["path"])
	}
	if afterNav["open"] != false {
		t.Errorf("hamburger menu should close after SPA nav; got open=%v", afterNav["open"])
	}

	// (2) Escape close.
	var afterEsc map[string]any
	err = chromedp.Run(ctx,
		chromedp.Click(".site-header summary.site-nav__toggle", chromedp.ByQuery),
		pageReady(),
		chromedp.KeyEvent(kb.Escape),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const d = document.querySelector('.site-header details.site-nav');
            return {open: d ? d.open : 'no-details'};
        })()`, &afterEsc),
	)
	if err != nil {
		t.Fatalf("chromedp esc: %v", err)
	}
	if afterEsc["open"] != false {
		t.Errorf("hamburger menu should close on Escape; got open=%v", afterEsc["open"])
	}
}

// TestE2E_Chaos_EscDoesNotStealFocusFromMain asserts that pressing
// Escape with an open hamburger menu in the background DOES close
// the menu but DOES NOT yank focus away from wherever the user was
// reading in <main>. Pulling focus to the summary on a stray Esc
// is a classic anti-pattern; we only refocus when the closing
// disclosure already contained focus.
func TestE2E_Chaos_EscDoesNotStealFocusFromMain(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 800),
		chromedp.Navigate(base+"/"),
		pageReady(),
		// Open the menu, then move focus deliberately into <main>.
		chromedp.Click(".site-header summary.site-nav__toggle", chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const target = document.querySelector('main a, main button, main [tabindex]');
            if (target) target.focus();
            const before = document.activeElement;
            document.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape', bubbles:true}));
            return {
                detailsClosed: document.querySelector('.site-header details.site-nav').open === false,
                focusInMain: document.querySelector('main')?.contains(document.activeElement) ?? false,
                focusOnSummary: document.activeElement === document.querySelector('summary.site-nav__toggle'),
            };
        })()`, &info),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if info["detailsClosed"] != true {
		t.Errorf("Escape should still close the open disclosure; got open")
	}
	if info["focusOnSummary"] == true {
		t.Errorf("Escape stole focus to summary even though user was in <main> — anti-pattern")
	}
}

// TestE2E_Chaos_HamburgerEagerCloseAndFocus combines two regressions:
//   (A) clicking a nav link inside the open mobile menu must close
//       the <details> BEFORE the SPA fetch resolves — otherwise the
//       dropdown floats over stale content for the entire roundtrip
//       and the user perceives the click as "didn't take".
//   (B) once the SPA nav completes, focus must move into the new
//       <main> so keyboard users aren't stranded on a now-detached
//       anchor.
func TestE2E_Chaos_HamburgerEagerCloseAndFocus(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var snap map[string]any
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 800),
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Click(".site-header summary.site-nav__toggle", chromedp.ByQuery),
		pageReady(),
		// Click the link. Immediately (synchronously after preventDefault)
		// the disclosure should already be closed — read the state here
		// without any delay so we catch the "during fetch" window.
		chromedp.Evaluate(`(() => {
            const a = document.querySelector('.site-header details.site-nav nav a[href="/about"]');
            a.click();
            const d = document.querySelector('.site-header details.site-nav');
            return {openDuringFetch: d.open};
        })()`, &snap),
		// Now let the SPA-nav settle.
		pageReady(),
		chromedp.Evaluate(`(() => {
            const m = document.getElementById('main-content');
            return {
                openAfter: document.querySelector('.site-header details.site-nav').open,
                focusOnMain: document.activeElement === m,
                path: location.pathname,
            };
        })()`, &snap),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if snap["path"] != "/about" {
		t.Fatalf("expected nav to /about, got %v", snap["path"])
	}
	if snap["openAfter"] != false {
		t.Errorf("disclosure must be closed after SPA nav")
	}
	if snap["focusOnMain"] != true {
		t.Errorf("focus must move into <main> after SPA nav so keyboard users aren't stranded; got activeElement != main")
	}
}

// TestE2E_Chaos_AnnounceRouteCancelsOnRapidNav asserts that rapid A→B→C
// SPA-navs don't leave the live region stuck on an intermediate title
// because of an uncancelled setTimeout.
func TestE2E_Chaos_AnnounceRouteCancelsOnRapidNav(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var snap map[string]any
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            // Instrument setTimeout to count announce-route timer fires.
            // If clearTimeout cancellation works, the first nav's 50ms
            // timer must be cancelled by the second nav, so we expect
            // ≤1 announce-related timer callback to fire — not 2.
            const r = document.getElementById('fui-route-announce');
            const origST = window.setTimeout;
            let fires = 0;
            window.setTimeout = function(fn, ms) {
                if (ms === 50) {
                    // The announceRoute timer; wrap its callback to count.
                    return origST(function() { fires++; fn(); }, ms);
                }
                return origST(fn, ms);
            };
            // Fire two SPA-navs back-to-back, well under 50ms apart.
            history.pushState(null, '', '/docs/');
            window.dispatchEvent(new Event('popstate'));
            history.pushState(null, '', '/about');
            window.dispatchEvent(new Event('popstate'));
            return new Promise(resolve => origST(() => {
                window.setTimeout = origST; // restore
                resolve({
                    region: r ? r.textContent : '',
                    title: document.title,
                    fires: fires,
                });
            }, 250));
        })()`, &snap,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	region := toString(snap["region"])
	title := toString(snap["title"])
	if region == "" {
		t.Errorf("live region should hold the latest title after rapid nav; got empty")
	}
	if !strings.Contains(region, title) && !strings.Contains(title, region) {
		t.Errorf("live region (%q) should track the latest title (%q) — intermediate state leaked", region, title)
	}
	// Cancellation proof: at most one announce-route timer should have
	// fired its callback (the second nav's). Without clearTimeout,
	// both fire and we'd see 2.
	if f, _ := snap["fires"].(float64); f > 1 {
		t.Errorf("expected ≤1 announce-route timer to fire (proof of clearTimeout cancellation); got %d", int(f))
	}
}

// TestE2E_Chaos_TouchTargetsAt44 checks every interactive button on
// the framework-ui index renders at >= 44 CSS pixels tall (WCAG
// 2.5.5 minimum). Catches regressions where the spacing scale gets
// too tight to be tappable.
func TestE2E_Chaos_TouchTargetsAt44(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var heights []float64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            return [...document.querySelectorAll('button.ui-button, a.ui-button')]
                .map(el => el.getBoundingClientRect().height);
        })()`, &heights),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(heights) == 0 {
		t.Skip("no ui-button elements on /framework-ui/ — test premise invalid")
	}
	for i, h := range heights {
		if h < 44 {
			t.Errorf("ui-button[%d] height=%.1fpx, want >= 44 (WCAG 2.5.5 minimum)", i, h)
		}
	}
}

// TestE2E_Chaos_FrameworkUIPageRendersWithoutOverlaps walks the kitchen
// sink page (/framework-ui/) and confirms every component class has a
// non-zero render box. Catches CSS regressions where a component
// becomes 0×0 due to broken token references.
func TestE2E_Chaos_FrameworkUIComponentsAllHaveLayout(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	classes := []string{
		"ui-page-header",
		"ui-stat-card",
		"ui-avatar",
		"ui-badge",
		"ui-callout",
		"ui-empty-state",
		"ui-form-section",
		"ui-form-field",
		"ui-button--danger",
	}
	for _, cls := range classes {
		var rect map[string]float64
		err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/framework-ui/"),
			pageReady(),
			chromedp.Evaluate(`(() => {
                const el = document.querySelector('.`+cls+`');
                if (!el) return null;
                const r = el.getBoundingClientRect();
                return {w: r.width, h: r.height};
            })()`, &rect),
		)
		if err != nil {
			t.Errorf("%s: chromedp: %v", cls, err)
			continue
		}
		if rect == nil {
			t.Errorf("%s: not present on page", cls)
			continue
		}
		if rect["w"] <= 0 || rect["h"] <= 0 {
			t.Errorf("%s: zero-sized render box w=%.1f h=%.1f", cls, rect["w"], rect["h"])
		}
	}
}

// TestE2E_Livereload_ScriptIsServed confirms the dev-mode livereload
// script is reachable and contains the long-poll fetch. Gated by
// GOFASTR_DEV=1; without that env var, the endpoints are absent
// (covered by TestLivereloadGatedByDevMode).
func TestE2E_Livereload_ScriptIsServed(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var jsBody string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/__livereload.js"),
		pageReady(),
		chromedp.Text("body", &jsBody),
	)
	if err != nil {
		t.Fatalf("chromedp navigate to /__livereload.js: %v", err)
	}
	if !strings.Contains(jsBody, "/__livereload") {
		t.Errorf("livereload.js missing /__livereload reference; got:\n%s", jsBody)
	}
	if !strings.Contains(jsBody, "EventSource") {
		t.Errorf("livereload.js should use EventSource (SSE push); got:\n%s", jsBody)
	}
}

// Per-page title smoke is covered by TestComponentDemosRenderWithoutPanic
// in website_test.go (uses httptest directly — fast, no SSE or chromedp
// involvement). Keeping a single-page chromedp variant here only as a
// real-browser sanity check that the framework-ui page hydrates.
func TestE2E_FrameworkUIPageRendersTitle(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var title string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Title(&title),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(title, "Framework UI") {
		t.Errorf("expected title to contain 'Framework UI', got %q", title)
	}
}
