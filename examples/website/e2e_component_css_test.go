package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestE2E_ComponentCSS_FirstPaintUsesBundle verifies that a page
// rendering multiple registered components ships exactly one bundled
// <link> in <head> (rather than N individual links) for first paint.
// See core-ui/ARCHITECTURE.md → Component CSS.
func TestE2E_ComponentCSS_FirstPaintUsesBundle(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hrefs []string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            return [...document.head.querySelectorAll('link[rel="stylesheet"]')]
                .map(l => l.getAttribute('href'));
        })()`, &hrefs),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	bundleCount := 0
	directCompCount := 0
	for _, h := range hrefs {
		switch {
		case strings.Contains(h, "/__gofastr/comp-bundle.css"):
			bundleCount++
		case strings.Contains(h, "/__gofastr/comp/"):
			directCompCount++
		}
	}
	if bundleCount != 1 {
		t.Errorf("expected exactly 1 bundle <link>, got %d (hrefs=%v)", bundleCount, hrefs)
	}
	if directCompCount != 0 {
		t.Errorf("framework-ui page should bundle, not ship %d direct comp links (hrefs=%v)", directCompCount, hrefs)
	}
}

// TestE2E_ComponentCSS_NoDuplicateLinksAfterNav verifies that
// navigating /a → /b → /a does NOT re-fetch already-loaded component
// stylesheets. The dedup guard (data-fui-style + _pendingLinks) must
// prevent re-injection.
func TestE2E_ComponentCSS_NoDuplicateLinksAfterNav(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var styleNames []string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		// Navigate to a different page that doesn't include all the same
		// components (an SPA partial swap).
		chromedp.Navigate(base+"/"),
		pageReady(),
		// Back to the kitchen sink — components should already be linked.
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		// After the cycle, every data-fui-style="<name>" must be unique.
		chromedp.Evaluate(`(() => {
            const links = [...document.querySelectorAll('link[data-fui-style]')];
            return links.map(l => l.getAttribute('data-fui-style'));
        })()`, &styleNames),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	seen := map[string]int{}
	for _, n := range styleNames {
		seen[n]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("component %q linked %d times — dedup failed", name, count)
		}
	}
}

// TestE2E_ComponentCSS_NoInlineStylesFromFramework verifies the rule
// that component CSS only ships as <link>, never as inline <style>.
// (The runtime's overlay CSS is allowed — that's a separate
// concern, identified by data-gofastr-overlays.)
func TestE2E_ComponentCSS_NoInlineStylesFromFramework(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var styleBlocks []map[string]string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            return [...document.head.querySelectorAll('style')].map(s => ({
                hasFuiAttr: s.hasAttribute('data-fui-comp') ? 'yes' : 'no',
                hasFuiStyle: s.hasAttribute('data-fui-style') ? 'yes' : 'no',
                overlay: s.hasAttribute('data-gofastr-overlays') ? 'yes' : 'no',
                preview: s.textContent.slice(0, 80),
            }));
        })()`, &styleBlocks),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for _, b := range styleBlocks {
		if b["hasFuiAttr"] == "yes" || b["hasFuiStyle"] == "yes" {
			t.Errorf("found inline <style> tagged as component CSS — must be <link>: %v", b)
		}
	}
}

// TestE2E_ComponentCSS_BundleURLContainsExpectedNames verifies the
// bundle URL lists components in sorted order and that the deployed
// components (PageHeader, DataTable, FormField, etc.) all show up.
func TestE2E_ComponentCSS_BundleURLContainsExpectedNames(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hrefs []string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            return [...document.head.querySelectorAll('link[rel="stylesheet"]')]
                .map(l => l.getAttribute('href'));
        })()`, &hrefs),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var bundle string
	for _, h := range hrefs {
		if strings.Contains(h, "/__gofastr/comp-bundle.css") {
			bundle = h
			break
		}
	}
	if bundle == "" {
		t.Fatal("no bundle link found")
	}
	// Required components on this page. PageHeader is LoadAlways so
	// it should appear even on pages that don't render it.
	required := []string{"ui-page-header", "ui-form-field", "ui-empty-state"}
	for _, name := range required {
		if !strings.Contains(bundle, name) {
			t.Errorf("bundle URL missing %q: %s", name, bundle)
		}
	}
	// Names should be sorted ASCII: ui-empty-state < ui-form-field < ui-page-header.
	iA, iB, iC := strings.Index(bundle, "ui-empty-state"),
		strings.Index(bundle, "ui-form-field"),
		strings.Index(bundle, "ui-page-header")
	if !(iA < iB && iB < iC) {
		t.Errorf("names not sorted in bundle URL (positions: empty-state=%d form-field=%d page-header=%d): %s",
			iA, iB, iC, bundle)
	}
}

// TestE2E_ComponentCSS_CatalogScriptInlined verifies the catalog JS
// is referenced (so the runtime can resolve marker → URL during
// hydration without an extra round-trip).
func TestE2E_ComponentCSS_CatalogScriptInlined(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hasCatalog bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            return typeof window.__gofastr_catalog === 'object' && window.__gofastr_catalog !== null;
        })()`, &hasCatalog),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasCatalog {
		t.Error("window.__gofastr_catalog not present after first paint")
	}
}
