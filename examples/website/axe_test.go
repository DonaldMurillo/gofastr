package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// =============================================================================
// axe-core accessibility coverage for every /components/<slug> page.
//
// We inject the vendored axe-core into the live page, call axe.run() with the
// default rule set scoped to the document body, and fail the test on any
// detected violation. axe-core covers WCAG 2.0/2.1 A/AA most-impactful rules
// (color-contrast, label, aria-required-*, image-alt, list, …) — well beyond
// what hand-rolled chromedp expressions can catch.
//
// The axe.min.js bundle ships in examples/website/testdata/ — go:embed below
// keeps the test hermetic so CI doesn't depend on the jsdelivr CDN.
//
// To skip a known-problematic rule: add it to axeRuleAllowlist with a comment
// explaining why it's deferred. To skip a specific page: add it to
// axePageAllowlist. The allowlist exists so review can override the gate
// deliberately; silent skips aren't possible — the page list comes from
// main.go at test time.
// =============================================================================

//go:embed testdata/axe.min.js
var axeMinJS string

// axeRuleAllowlist names axe-core rule IDs that are deliberately skipped
// (passing { runOnly: { type: "rule", values: [...] } } the inverse list is
// awkward; instead we DROP allowed-violations after axe.run() reports them).
//
// Add entries here with a short comment when a rule is intentionally deferred
// or known-broken in our test fixtures.
var axeRuleAllowlist = map[string]string{
	// landmark-unique: multiple instances of the SAME landmark
	// component on one demo page IS the demo's purpose — showing
	// horizontal + vertical ProgressSteps side-by-side, two
	// Paginations stacked, etc. In real app pages a single instance
	// per landmark name is the expected pattern. The framework
	// components themselves do the right thing (one aria-label per
	// instance); the duplication is a /components/<slug> demo
	// construct. App-level a11y audits should still enforce this rule.
	"landmark-unique": "demos legitimately render multiple instances of one landmark component",
}

// axePageAllowlist names component slugs (e.g. "menu", "modal") whose pages
// open a transient widget that axe can't easily measure (focus traps move
// the active element out of the normal flow during scan). Empty by default.
var axePageAllowlist = map[string]string{}

// axeViolation matches the shape axe-core returns.
type axeViolation struct {
	ID          string           `json:"id"`
	Impact      string           `json:"impact"`
	Description string           `json:"description"`
	Help        string           `json:"help"`
	HelpURL     string           `json:"helpUrl"`
	Tags        []string         `json:"tags"`
	Nodes       []axeViolatedNode `json:"nodes"`
}

type axeViolatedNode struct {
	HTML   string   `json:"html"`
	Target []string `json:"target"`
}

// newAxeBrowser returns one chromedp browser context shared across
// all axe runs in a single test. Per-page contexts blow through the
// websocket dial deadline when we audit ≥20 pages in a row.
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

// runAxeIn opens a FRESH tab in the shared browser for each page so
// the previous page's long-lived SSE socket gets torn down — without
// fresh tabs the SSE streams stack until chrome's event loop chokes
// after ~20 pages. axe-core gets injected into the new tab, runs the
// default rule set, returns the parsed violations (after allowlist
// filtering). Per-page operations get their own deadline so one
// slow page can't break the audit of the others.
func runAxeIn(t *testing.T, browser context.Context, base, path string) []axeViolation {
	t.Helper()
	tabCtx, tabCancel := chromedp.NewContext(browser)
	defer tabCancel()
	// 30s per page. axe.run() is the slow part for big pages — JSON
	// serialization of large violation arrays can also add a beat.
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

// evalAwaitPromise tells chromedp.Evaluate to wait for the returned
// Promise to resolve before returning the value. Required so axe.run()
// (which is async) returns its violations array to Go, not a Promise
// remote-object handle.
func evalAwaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	p.AwaitPromise = true
	return p
}

// pagesFromMainGo returns every /components/<slug> route registered in
// main.go. The full slug list comes from the source so this drift-aware:
// adding a new component page automatically gets axe coverage too.
func pagesFromMainGo(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	rx := regexp.MustCompile(`site\.Register\("(/components/[a-z0-9-]*)"`)
	matches := rx.FindAllStringSubmatch(string(data), -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		p := m[1]
		// Skip the index page itself + any allowlisted slugs.
		if p == "/components/" {
			continue
		}
		slug := strings.TrimPrefix(p, "/components/")
		if _, ok := axePageAllowlist[slug]; ok {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// TestAxe_AllComponentPagesAreClean is the gate. One failure per page max —
// the test prints every page's violations before failing, so authors see the
// full slate, not the first one.
func TestAxe_AllComponentPagesAreClean(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	base := startE2EServer(t)
	pages := pagesFromMainGo(t)
	if len(pages) == 0 {
		t.Fatal("no component pages discovered — drift test for axe is misconfigured")
	}
	browser := newAxeBrowser(t)
	// Force chrome to actually start before the timed loop kicks in.
	// chromedp lazy-starts Chrome on first action; a 60s warm-up
	// window covers process exec + initial websocket handshake.
	warm, warmCancel := context.WithTimeout(browser, 60*time.Second)
	defer warmCancel()
	if err := chromedp.Run(warm, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("chrome warm-up failed: %v", err)
	}
	type pageResult struct {
		path       string
		violations []axeViolation
	}
	results := make([]pageResult, 0, len(pages))
	for _, p := range pages {
		violations := runAxeIn(t, browser, base, p)
		results = append(results, pageResult{path: p, violations: violations})
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
			t.Errorf("    %s", v.HelpURL)
			for _, n := range v.Nodes {
				snippet := n.HTML
				if len(snippet) > 200 {
					snippet = snippet[:200] + "…"
				}
				t.Errorf("    target=%v", n.Target)
				t.Errorf("    html=%s", snippet)
			}
		}
	}
	if any {
		t.Errorf("\nfix the violations OR (if intentionally deferred) add the rule id " +
			"to axeRuleAllowlist with a justification comment in axe_test.go.")
	}
}
