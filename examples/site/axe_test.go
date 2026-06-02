package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// =============================================================================
// axe-core accessibility gate for every site page.
//
// Injects vendored axe-core into each live page, runs the default rule set,
// and fails on any violation. Covers WCAG 2.0/2.1 A/AA most-impactful rules
// (color-contrast, label, aria-*, heading-order, link-in-text-block, …).
//
// To defer a rule: add it to axeRuleAllowlist with a justification. To skip a
// page: add its slug to axePageAllowlist. The bar is zero un-allowlisted
// violations across the whole catalog + key static pages.
// =============================================================================

//go:embed testdata/axe.min.js
var axeMinJS string

// axeRuleAllowlist names axe-core rule IDs that are deliberately skipped, with
// a justification. Kept deliberately tiny — these are demo CONSTRUCTS, not
// real-app a11y debt.
var axeRuleAllowlist = map[string]string{
	// landmark-unique: a /components/<slug> demo legitimately renders MULTIPLE
	// instances of one landmark component (two Paginations stacked, sidebar +
	// TOC, etc.). Real app pages render one per landmark name; the duplication
	// is a gallery construct. The components themselves do the right thing.
	"landmark-unique": "demos render multiple instances of one landmark component on purpose",
	// landmark-complementary-is-top-level: ui.Callout (info variant) and
	// ui.Sidebar deliberately render complementary/aside landmarks (the
	// framework's own tests mandate <aside role="complementary"> for info
	// callouts). On content + /components/<slug> demo pages they appear nested
	// inside <main> rather than as a top-level region — a content/demo
	// construct, not an app-structure barrier (the content stays reachable and
	// labelled). A real app places one complementary region at the top level.
	"landmark-complementary-is-top-level": "framework Callout/Sidebar landmarks used inline in content/demos, not as top-level regions",
	// heading-order: the /components gallery shows each component in ISOLATION,
	// so the component's own internal heading (Card heading <h3>, EmptyState
	// title <h3>, Dropzone label <h3>) and nav-rail labels (<h6>) sit directly
	// under the page <h1> with no intervening <h2>. In a real page the
	// component lives inside a section <h2>, so the level doesn't skip — the
	// skip is a gallery construct, not a content-authoring defect. Content
	// pages (home, docs, philosophy) use a proper h1→h2→h3 outline.
	"heading-order": "gallery shows components in isolation, so their internal headings sit directly under the page h1",
}

// axePageAllowlist names component slugs whose pages open a transient widget
// axe can't measure (focus traps move the active element). Empty by default.
var axePageAllowlist = map[string]string{}

type axeViolation struct {
	ID          string            `json:"id"`
	Impact      string            `json:"impact"`
	Description string            `json:"description"`
	Help        string            `json:"help"`
	HelpURL     string            `json:"helpUrl"`
	Tags        []string          `json:"tags"`
	Nodes       []axeViolatedNode `json:"nodes"`
}

type axeViolatedNode struct {
	HTML   string   `json:"html"`
	Target []string `json:"target"`
}

// newAxeBrowser returns one chromedp browser context shared across all axe
// runs in a single test (per-page browsers blow the websocket dial deadline
// when auditing many pages in a row).
func newAxeBrowser(t *testing.T) context.Context {
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

// runAxeIn opens a FRESH tab per page (so the previous page's SSE socket is
// torn down), injects axe-core, runs it, and returns allowlist-filtered
// violations. Each page gets its own deadline.
func runAxeIn(t *testing.T, browser context.Context, base, path string) []axeViolation {
	t.Helper()
	tabCtx, tabCancel := chromedp.NewContext(browser)
	defer tabCancel()
	ctx, cancel := context.WithTimeout(tabCtx, 30*time.Second)
	defer cancel()
	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+path),
		pageReady(),
		chromedp.Evaluate(";"+axeMinJS, nil),
		chromedp.Evaluate(`(async () => {
			const r = await axe.run(document, {
				resultTypes: ['violations'],
				rules: { 'region': { enabled: false } } // landmark regions vary across demo pages
			});
			return JSON.stringify(r.violations);
		})()`, &raw, evalAwaitPromise),
	)
	if err != nil {
		t.Errorf("axe on %s: %v", path, err)
		return nil
	}
	var vs []axeViolation
	if err := json.Unmarshal([]byte(raw), &vs); err != nil {
		t.Errorf("axe %s: parse violations: %v\nraw=%s", path, err, raw)
		return nil
	}
	var kept []axeViolation
	for _, v := range vs {
		if _, ok := axeRuleAllowlist[v.ID]; ok {
			continue
		}
		kept = append(kept, v)
	}
	return kept
}

// evalAwaitPromise makes chromedp.Evaluate await the returned Promise so
// axe.run() resolves to the violations array, not a Promise handle.
func evalAwaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	p.AwaitPromise = true
	return p
}

// axePages returns every /components/<slug> route (from the catalog) plus the
// key static + docs surfaces. Routes are generated from componentCatalog in
// main.go, so iterating the catalog keeps this in lock-step (no drift).
func axePages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range componentCatalog {
		if _, ok := axePageAllowlist[c.Slug]; ok {
			continue
		}
		out = append(out, "/components/"+c.Slug)
	}
	out = append(out,
		"/", "/get-started", "/docs/", "/docs/entity-declarations",
		"/examples", "/philosophy", "/seo", "/seo-bundle", "/components/",
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
	browser := newAxeBrowser(t)
	// Start Chrome on the LONG-LIVED browser context (a short-lived timeout
	// child would kill the browser when it expires → later pages cancel).
	if err := chromedp.Run(browser, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("chrome warm-up failed: %v", err)
	}
	type pageResult struct {
		path       string
		violations []axeViolation
	}
	results := make([]pageResult, 0, len(pages))
	for _, p := range pages {
		results = append(results, pageResult{path: p, violations: runAxeIn(t, browser, base, p)})
	}
	any := false
	for _, r := range results {
		if len(r.violations) == 0 {
			continue
		}
		any = true
		t.Errorf("axe violations on %s:", r.path)
		for _, v := range r.violations {
			t.Errorf("  • [%s · %s] %s", v.ID, v.Impact, v.Help)
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
