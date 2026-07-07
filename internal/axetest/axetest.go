// Package axetest is a shared axe-core accessibility-testing harness for
// GoFastr example apps and the framework's own chromedp suites.
//
// It vendors the axe-core engine (testdata/axe.min.js, embedded) and exposes
// the primitives every a11y gate needs:
//
//   - [NewBrowser] — one chromedp browser context reused across all scans;
//   - [NewTab] — a fresh tab per page so the previous page's SSE socket tears down;
//   - [Prepare] — the load-bearing color-scheme freeze + force step;
//   - [Scan] — injects axe and runs it against the CURRENT DOM state (so a
//     caller can open a modal first and then scan).
//
// Each app keeps its own page list, allowlist, and gate Test function — only
// the reusable machinery lives here.
package axetest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// axeMinJS is the vendored axe-core engine, embedded so the gate is hermetic
// (CI never reaches out to a CDN). It is minified JavaScript SOURCE (text),
// not a compiled binary, so it is exempt from the "never commit binaries" rule.
//
//go:embed testdata/axe.min.js
var axeMinJS string

// Schemes lists the color schemes every page is scanned under, forced via the
// same <html data-color-scheme> attribute that ui.ThemeToggle flips. Without
// forcing, the scheme bootstrap follows the host's prefers-color-scheme — a
// dev machine in Dark appearance only ever audits the dark palette while CI
// runners (light by default) only audit light, so contrast regressions in the
// unseen scheme stay invisible locally and surface as CI-only failures.
var Schemes = []string{"dark", "light"}

// Violation is one axe-core rule violation. Scheme records which forced color
// scheme produced it; it is set by [Scan], not part of the axe JSON.
type Violation struct {
	ID          string          `json:"id"`
	Impact      string          `json:"impact"`
	Description string          `json:"description"`
	Help        string          `json:"help"`
	HelpURL     string          `json:"helpUrl"`
	Tags        []string        `json:"tags"`
	Nodes       []ViolatedNode  `json:"nodes"`

	// Scheme is the forced color scheme that produced the violation.
	// Set by Scan, not part of the axe JSON payload.
	Scheme string `json:"-"`
}

// ViolatedNode is one element that tripped a rule.
type ViolatedNode struct {
	HTML   string   `json:"html"`
	Target []string `json:"target"`
}

// NewBrowser returns one chromedp browser context shared across all axe runs
// in a single test. Per-page browsers blow the websocket dial deadline when
// auditing many pages in a row, so reuse one browser and open fresh tabs with
// [NewTab]. The returned context is long-lived (no per-scan timeout child) —
// a timeout child would kill the browser when it expired and cancel later pages.
func NewBrowser(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	return browserCtx
}

// NewTab opens a FRESH tab derived from browser (so the previous page's SSE
// socket is torn down) with a per-tab scan timeout. It returns the tab context
// and a cancel func the caller MUST defer — cancelling tears down both the
// timeout and the tab target so sockets don't leak across pages.
func NewTab(t *testing.T, browser context.Context) (context.Context, context.CancelFunc) {
	t.Helper()
	tabCtx, tabCancel := chromedp.NewContext(browser)
	ctx, cancel := context.WithTimeout(tabCtx, 30*time.Second)
	return ctx, func() { cancel(); tabCancel() }
}

// Prepare is a chromedp action that freezes CSS transitions/animations and
// forces the given color scheme on the current document. Run it AFTER
// navigating (and a brief settle), BEFORE any widget interaction and [Scan].
//
// The freeze is load-bearing AND must be a constructed stylesheet: the scheme
// flip starts 120–160ms color transitions (header links, search pill), and in
// throttled headless tabs animation frames may never tick, pinning computed
// colors at the PREVIOUS scheme's values indefinitely — axe then reports
// phantom mixed-scheme contrast failures on every page. An injected <style>
// element cannot fix this when the host ships a `default-src 'self'` CSP,
// which silently blocks inline styles; adoptedStyleSheets is script-created
// and not subject to style-src, so it bypasses the CSP.
func Prepare(scheme string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		// Freeze transitions via a constructed stylesheet (CSP-safe).
		if err := chromedp.Evaluate(`(() => {
			const s = new CSSStyleSheet();
			s.replaceSync('*, *::before, *::after { transition: none !important; animation: none !important; }');
			document.adoptedStyleSheets = [...document.adoptedStyleSheets, s];
		})()`, nil).Do(ctx); err != nil {
			return err
		}
		// Force the color scheme via the same attribute ui.ThemeToggle flips.
		if err := chromedp.Evaluate(fmt.Sprintf(
			`document.documentElement.setAttribute("data-color-scheme", %q);`, scheme), nil).Do(ctx); err != nil {
			return err
		}
		// Settle so any scheme-attribute listeners run before axe measures.
		return chromedp.Sleep(150 * time.Millisecond).Do(ctx)
	})
}

// ScanOption modifies how [Scan] configures axe.run(). The nil option is a
// no-op; construct one with [WithDisabledRules] or [WithEnabledRules].
type ScanOption func(*scanConfig)

// scanConfig is the resolved set of axe.run() `rules` overrides.
type scanConfig struct {
	disabledRules []string
	enabledRules  []string
}

// WithDisabledRules passes the given axe rule IDs to axe.run() as disabled
// (not evaluated at all). Use only when a rule can structurally never apply
// to the host's surfaces; a ruleAllowlist entry is preferred so the skip
// stays visible in each app's test source.
func WithDisabledRules(rules ...string) ScanOption {
	return func(c *scanConfig) { c.disabledRules = append(c.disabledRules, rules...) }
}

// WithEnabledRules turns ON axe rule IDs that ship disabled-by-default —
// notably the WCAG 2.2 `target-size` rule (24px minimum tap target, tagged
// wcag22aa/wcag258), which axe-core evaluates only when explicitly enabled.
// A caller that passes no options keeps axe's defaults verbatim (target-size
// stays off), so existing gates are unaffected.
func WithEnabledRules(rules ...string) ScanOption {
	return func(c *scanConfig) { c.enabledRules = append(c.enabledRules, rules...) }
}

// Scan injects axe-core and runs it once against the CURRENT DOM state,
// returning allowlist-filtered [Violation]s tagged with scheme. The caller
// controls navigation, [Prepare](scheme), and any widget opening before
// calling — Scan only measures whatever the page looks like right now, which
// is what lets a gate open a modal first and then scan the open-widget DOM.
//
// ruleAllowlist maps axe rule IDs to skip (ID → justification) — a Violation
// for an allowlisted ID is dropped from the result. Behavior modifiers are
// passed as [ScanOption] values: [WithDisabledRules] (a rule that can
// structurally never apply, e.g. `region` on a fragment demo) and
// [WithEnabledRules] (a default-off rule to turn on, e.g. WCAG 2.2
// `target-size`). A caller that passes neither gets axe's stock behavior.
func Scan(ctx context.Context, scheme string, ruleAllowlist map[string]string, opts ...ScanOption) ([]Violation, error) {
	var cfg scanConfig
	for _, o := range opts {
		o(&cfg)
	}

	// Guard against vacuous passes. axe evaluates the CURRENT DOM, so a route
	// that broke and serves an empty <body> scans as ZERO violations — the gate
	// turns green on a page that rendered nothing. Before injecting axe, assert
	// the page actually rendered: a real screen mounts dozens of elements under
	// <body>; a blank/500 shell sits well under minBodyElements. Fail loudly
	// (callers t.Fatalf on error) so a broken screen can't hide behind the gate.
	var elementCount int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('body *').length`, &elementCount),
	); err != nil {
		return nil, fmt.Errorf("axe pre-scan (%s scheme): population check: %w", scheme, err)
	}
	if elementCount < minBodyElements {
		return nil, fmt.Errorf("axe pre-scan (%s scheme): page rendered only %d elements under <body> (need ≥%d) — the page is blank or not rendered; refusing a vacuous pass", scheme, elementCount, minBodyElements)
	}

	rulesJS := axeRulesJS(cfg)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(";"+axeMinJS, nil),
		chromedp.Evaluate(`(async () => {
			const r = await axe.run(document, {
				resultTypes: ['violations'],
				rules: `+rulesJS+`
			});
			return JSON.stringify(r.violations);
		})()`, &raw, evalAwaitPromise),
	); err != nil {
		return nil, fmt.Errorf("axe scan (%s scheme): %w", scheme, err)
	}
	var vs []Violation
	if err := json.Unmarshal([]byte(raw), &vs); err != nil {
		return nil, fmt.Errorf("parse axe violations (%s scheme): %w\nraw=%s", scheme, err, raw)
	}
	var kept []Violation
	for _, v := range vs {
		if _, ok := ruleAllowlist[v.ID]; ok {
			continue
		}
		v.Scheme = scheme
		kept = append(kept, v)
	}
	return kept, nil
}

// minBodyElements is the floor below which a page is treated as blank / not
// rendered. A real screen renders far more than this; the threshold is a trip-
// wire for an empty shell, not a meaningful content minimum.
const minBodyElements = 5

// axeRulesJS renders the axe.run() `rules` option JS object from a
// scanConfig: every enabled rule is forced on, every disabled rule is forced
// off, and every other rule keeps its axe-core default. Empty input → "{}"
// (axe evaluates every default-enabled rule and skips default-disabled ones
// like target-size — so a caller with no options is byte-for-byte unchanged).
func axeRulesJS(c scanConfig) string {
	if len(c.enabledRules) == 0 && len(c.disabledRules) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{ ")
	first := true
	emit := func(rule, state string) {
		if !first {
			b.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&b, "%q: { enabled: %s }", rule, state)
	}
	for _, r := range c.enabledRules {
		emit(r, "true")
	}
	for _, r := range c.disabledRules {
		emit(r, "false")
	}
	b.WriteString(" }")
	return b.String()
}

// evalAwaitPromise makes chromedp.Evaluate await the returned Promise so
// axe.run() resolves to the violations array, not a Promise handle.
func evalAwaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	p.AwaitPromise = true
	return p
}
