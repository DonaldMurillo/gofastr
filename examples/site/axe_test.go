package main

// =============================================================================
// axe-core accessibility gate for every site page.
//
// The reusable harness lives in internal/axetest: it embeds the vendored
// axe engine, exports the Violation types, the chromedp browser/tab factories,
// the load-bearing color-scheme Prepare step, and Scan (inject axe, run it
// against the current DOM, filter by allowlist). This file keeps only what is
// specific to the site gate: the page list, the allowlist with its
// justifications, and the gate Test.
//
// Every page is scanned under BOTH color schemes (see axetest.Schemes) so the
// result does not depend on the host OS appearance / prefers-color-scheme.
//
// To defer a rule: add it to axeRuleAllowlist with a justification. To skip a
// page: add its slug to axePageAllowlist. The bar is zero un-allowlisted
// violations across the whole catalog + key static pages.
// =============================================================================

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/axetest"
)

// axeComponentsAllowlist names axe-core rule IDs skipped ONLY on
// /components/* gallery pages. These are demo CONSTRUCTS (isolated components
// stacked on one page), not real-app a11y debt — and they do NOT apply
// anywhere else. Content, docs, and static pages scan with an EMPTY allowlist
// so a real heading-order / landmark defect there actually surfaces instead of
// being silently dropped by a gallery-scoped rule.
var axeComponentsAllowlist = map[string]string{
	// landmark-unique: a /components/<slug> demo legitimately renders MULTIPLE
	// instances of one landmark component (two Paginations stacked, sidebar +
	// TOC, etc.). Real app pages render one per landmark name; the duplication
	// is a gallery construct. The components themselves do the right thing.
	"landmark-unique": "demos render multiple instances of one landmark component on purpose",
	// landmark-complementary-is-top-level: ui.Callout (info variant),
	// ui.AnchoredRail, and ui.StepRail deliberately render complementary
	// landmarks (the framework's own tests mandate <aside role="complementary">
	// for info callouts). On /components/<slug> demo pages they appear nested
	// inside <main> rather than as a top-level region — a demo construct, not
	// an app-structure barrier (the content stays reachable and labelled). A
	// real app places one complementary region at the top level.
	//
	// NOTE: ui.Sidebar is NOT part of this justification — it renders a plain
	// <div>, not an <aside>, so it contributes no complementary landmark.
	"landmark-complementary-is-top-level": "framework Callout/AnchoredRail/StepRail landmarks used inline in component demos, not as top-level regions",
	// heading-order: the /components gallery shows each component in ISOLATION,
	// so the component's own internal heading (Card heading <h3>, EmptyState
	// title <h3>, Dropzone label <h3>) and nav-rail labels (<h6>) sit directly
	// under the page <h1> with no intervening <h2>. In a real page the
	// component lives inside a section <h2>, so the level doesn't skip — the
	// skip is a gallery construct, not a content-authoring defect.
	"heading-order": "gallery shows components in isolation, so their internal headings sit directly under the page h1",
}

// axeIsComponentsPage reports whether path is a component-gallery demo page —
// the only place the gallery-construct allowlist + region disable apply.
func axeIsComponentsPage(path string) bool {
	return strings.HasPrefix(path, "/components/")
}

// axeAllowlistFor returns the rule allowlist for a page: the gallery-construct
// skips apply only to /components/* pages; every other page scans with an empty
// allowlist so a real heading-order / landmark defect on content or docs
// surfaces instead of being masked by a gallery rule.
func axeAllowlistFor(path string) map[string]string {
	if axeIsComponentsPage(path) {
		return axeComponentsAllowlist
	}
	return nil
}

// axeScanOptsFor returns the ScanOption set for a page. The structurally-
// inapplicable `region` rule (a component-isolation demo may mount a fragment
// with no <main>) is disabled only on /components/* pages; content + docs
// pages evaluate it. WCAG 2.2 `target-size` is enabled on every page.
func axeScanOptsFor(path string) []axetest.ScanOption {
	if axeIsComponentsPage(path) {
		return []axetest.ScanOption{
			axetest.WithDisabledRules("region"),
			axetest.WithEnabledRules("target-size"),
		}
	}
	return []axetest.ScanOption{axetest.WithEnabledRules("target-size")}
}

// axePageAllowlist names component slugs whose pages open a transient widget
// axe can't measure (focus traps move the active element). Empty by default.
var axePageAllowlist = map[string]string{}

// runAxeIn scans one page under every axetest.Schemes entry and returns
// allowlist-filtered violations tagged with the scheme that produced them.
func runAxeIn(t *testing.T, browser context.Context, base, path string) []axetest.Violation {
	t.Helper()
	var kept []axetest.Violation
	for _, scheme := range axetest.Schemes {
		kept = append(kept, runAxeScheme(t, browser, base, path, scheme)...)
	}
	return kept
}

// runAxeScheme opens a FRESH tab (so the previous page's SSE socket is torn
// down), settles the page, freezes transitions, forces the color scheme, then
// scans the current DOM state at the browser's default desktop viewport
// (1280×800, set on the shared browser). The freeze/force + scan details live
// in internal/axetest (Prepare + Scan); this wrapper owns only the per-page
// navigation + error tagging the gate's failure output depends on.
func runAxeScheme(t *testing.T, browser context.Context, base, path, scheme string) []axetest.Violation {
	t.Helper()
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+path),
		pageReady(),
		axetest.Prepare(scheme),
	); err != nil {
		t.Errorf("axe setup on %s (%s): %v", path, scheme, err)
		return nil
	}
	vs, err := axetest.Scan(ctx, scheme, axeAllowlistFor(path), axeScanOptsFor(path)...)
	if err != nil {
		t.Errorf("axe on %s (%s): %v", path, scheme, err)
		return nil
	}
	return vs
}

// runAxeMobileScheme mirrors runAxeScheme but emulates a 390×844 mobile
// viewport (iPhone-14-Pro-ish) before navigating, so responsive layouts are
// audited at mobile width. WCAG 2.2 target-size is the rule most likely to
// surface here (dense rows collapsing tap targets below 24px).
func runAxeMobileScheme(t *testing.T, browser context.Context, base, path, scheme string) []axetest.Violation {
	t.Helper()
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(base+path),
		pageReady(),
		axetest.Prepare(scheme),
	); err != nil {
		t.Errorf("axe setup on %s (%s, mobile): %v", path, scheme, err)
		return nil
	}
	vs, err := axetest.Scan(ctx, scheme, axeAllowlistFor(path), axeScanOptsFor(path)...)
	if err != nil {
		t.Errorf("axe on %s (%s, mobile): %v", path, scheme, err)
		return nil
	}
	return vs
}

// axePages returns every /components/<slug> route (from the catalog) plus the
// key static surfaces and EVERY registered /docs/<slug> page. Docs routes are
// generated from flatDocs() in docs_catalog.go — the same source registerScreens
// iterates to mount the routes — so a newly added doc is scanned automatically
// (no drift between "routes that exist" and "routes scanned"). /kiln is a
// standalone marketing page, not a docs entry, so it is listed explicitly.
//
// Scanning the full docs catalog (~30 pages × 2 schemes) adds roughly a minute
// to the suite versus the old single-page sample; that is the cost of not
// letting a docs page regress un-scanned, and it is acceptable.
func axePages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range componentCatalog {
		if _, ok := axePageAllowlist[c.Slug]; ok {
			continue
		}
		out = append(out, "/components/"+c.Slug)
	}
	// Every registered docs page, derived from the same catalog the routes
	// come from (flatDocs), so the gate and the router cannot drift.
	for _, d := range flatDocs() {
		out = append(out, "/docs/"+d.Slug)
	}
	out = append(out,
		"/", "/get-started", "/docs/", "/examples", "/kiln",
		"/philosophy", "/seo", "/seo-bundle", "/components/",
	)
	sort.Strings(out)
	return out
}

// TestAxe_AllPagesAreClean is the gate. It prints every page's violations
// before failing so the full slate is visible, not just the first.
func TestAxe_AllPagesAreClean(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	base := startE2EServer(t)
	pages := axePages(t)
	if len(pages) == 0 {
		t.Fatal("no pages discovered — axe gate is misconfigured")
	}
	browser := axetest.NewBrowser(t)
	// Start Chrome on the LONG-LIVED browser context (a short-lived timeout
	// child would kill the browser when it expires → later pages cancel).
	if err := chromedp.Run(browser, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("chrome warm-up failed: %v", err)
	}
	type pageResult struct {
		path       string
		viewport   string // "desktop" (1280) or "mobile" (390)
		violations []axetest.Violation
	}
	var results []pageResult
	// Desktop pass: every page × both schemes at the browser's 1280×800 viewport.
	for _, p := range pages {
		results = append(results, pageResult{path: p, viewport: "desktop", violations: runAxeIn(t, browser, base, p)})
	}
	// Mobile pass: curated subset × both schemes at 390×844. The full matrix
	// would double the suite, so only the pages whose responsive layout is most
	// likely to collapse tap targets below the 24px WCAG 2.2 target-size floor.
	mobileSubset := []string{
		"/", "/get-started", "/components/", "/components/datatable",
		"/components/filtertoolbar", "/components/multiselect",
		"/components/toggleaction", "/components/pagination",
	}
	for _, p := range mobileSubset {
		for _, scheme := range axetest.Schemes {
			results = append(results, pageResult{
				path:       p,
				viewport:   "mobile",
				violations: runAxeMobileScheme(t, browser, base, p, scheme),
			})
		}
	}
	any := false
	for _, r := range results {
		if len(r.violations) == 0 {
			continue
		}
		any = true
		t.Errorf("axe violations on %s [%s]:", r.path, r.viewport)
		for _, v := range r.violations {
			t.Errorf("  • [%s · %s · %s scheme] %s", v.ID, v.Impact, v.Scheme, v.Help)
			for _, n := range v.Nodes {
				snippet := n.HTML
				if len(snippet) > 160 {
					snippet = snippet[:160] + "…"
				}
				t.Errorf("    target=%v  html=%s", n.Target, snippet)
			}
		}
	}
	if any {
		t.Errorf("\nfix the violations OR (if a genuine demo construct) add the rule id " +
			"to axeRuleAllowlist with a justification in axe_test.go.")
	}
}
