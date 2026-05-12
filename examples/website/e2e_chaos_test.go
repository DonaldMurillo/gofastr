package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
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
