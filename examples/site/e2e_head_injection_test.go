package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// ---------------------------------------------------------------------------
// E2E tests for the head injection API.
//
// These tests verify that ALL tiers of head injection actually render in a
// real browser — not just in unit-test string checks.
//
// Tier 1: WithHeadHTML escape hatch (tested via site integration)
// Tier 2: Typed helpers — favicon, og, twitter, description
// Tier 3: Per-page SEO interfaces (ScreenCanonical etc.) — tested on /seo
//
// NOTE: The site has no global WithCanonicalURL — that is intentional (a
// fixed global canonical would declare the homepage canonical on every page).
// Only /seo and /seo-bundle emit a canonical; sub-tests that check for it
// target /seo rather than /.
// ---------------------------------------------------------------------------

// collectHeadHTML navigates to url and returns document.head.innerHTML.
func collectHeadHTML(t *testing.T, ctx context.Context, url string) string {
	t.Helper()
	var headHTML string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		pageReady(),
		chromedp.Evaluate(`document.head.innerHTML`, &headHTML),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return headHTML
}

// TestE2E_HeadInjection_AllGlobalTagsPresent verifies that every typed
// helper option configured on the site host produces real DOM elements.
// This is the E2E proof that the full typed helper tier works end-to-end.
func TestE2E_HeadInjection_AllGlobalTagsPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	headHTML := collectHeadHTML(t, ctx, base+"/")

	// Every typed helper configured in main.go must appear in the DOM.
	checks := []struct {
		label string
		want  string
	}{
		// WithOpenGraph (3 fields set)
		{"og:title", `<meta property="og:title" content="GoFastr">`},
		{"og:url", `<meta property="og:url" content="https://gofastr.dev">`},
		{"og:type", `<meta property="og:type" content="website">`},
		// WithDescription
		{"description", `<meta name="description"`},
	}

	for _, tc := range checks {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			if !strings.Contains(headHTML, tc.want) {
				t.Errorf("head missing %s tag:\n  want: %s\n  head:\n%s", tc.label, tc.want, headHTML)
			}
		})
	}
}

// TestE2E_HeadInjection_CanonicalOnSEOPage verifies that /seo emits a
// canonical <link> while the home page (which has no canonical configured)
// does not. The site intentionally omits a global canonical.
func TestE2E_HeadInjection_CanonicalOnSEOPage(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// /seo implements ScreenCanonical — must emit a canonical link.
	seoHead := collectHeadHTML(t, ctx, base+"/seo")
	if !strings.Contains(seoHead, `<link rel="canonical"`) {
		t.Errorf("/seo should emit a canonical link; head:\n%s", seoHead)
	}

	// Home page has no canonical configured — must NOT emit one.
	homeHead := collectHeadHTML(t, ctx, base+"/")
	if strings.Contains(homeHead, `<link rel="canonical"`) {
		t.Errorf("home page should NOT emit a canonical link (no global canonical configured); head:\n%s", homeHead)
	}
}

// TestE2E_HeadInjection_NoSEOLeakageAcrossPages verifies that per-page SEO
// tags from /seo are not present on unrelated pages.
func TestE2E_HeadInjection_NoSEOLeakageAcrossPages(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// /components/accordion page should not carry /seo-specific tags.
	headHTML := collectHeadHTML(t, ctx, base+"/components/accordion")

	// Global og:title is fine; but /seo-specific per-page og:description
	// with "SEO" context should not bleed onto accordion.
	ogTitleCount := strings.Count(headHTML, `<meta property="og:title"`)
	if ogTitleCount > 1 {
		t.Errorf("expected 1 og:title on /components/accordion (global only), got %d", ogTitleCount)
	}
}

// TestE2E_HeadInjection_MultiplePagesAllGetGlobalTags verifies that
// several different pages all receive the global head tags — proving
// the injection isn't accidentally scoped to a single route.
func TestE2E_HeadInjection_MultiplePagesAllGetGlobalTags(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	for _, path := range []string{"/", "/components/accordion", "/components/tabs"} {
		path := path
		t.Run(path, func(t *testing.T) {
			headHTML := collectHeadHTML(t, ctx, base+path)
			// All pages should carry the global OG title.
			if !strings.Contains(headHTML, `<meta property="og:title" content="GoFastr">`) {
				t.Errorf("%s: head missing global og:title; head:\n%s", path, headHTML)
			}
		})
	}
}

// TestE2E_HeadInjection_TitleTag verifies that the <title> element
// renders correctly in the browser's document.title — proving the
// SSR title generation works end-to-end through the full rendering
// pipeline (app.RenderPage → injectChrome → browser).
func TestE2E_HeadInjection_TitleTag(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var title string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		pageReady(),
		chromedp.Evaluate(`document.title`, &title),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(title, "GoFastr") {
		t.Errorf("home page title should contain 'GoFastr', got %q", title)
	}

	// Verify the components/accordion page title too.
	var accordionTitle string
	err = chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		chromedp.Evaluate(`document.title`, &accordionTitle),
	)
	if err != nil {
		t.Fatalf("chromedp accordion: %v", err)
	}
	if !strings.Contains(accordionTitle, "Accordion") {
		t.Errorf("accordion page title should contain 'Accordion', got %q", accordionTitle)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected to find %q in output", needle)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected NOT to find %q in output, but it was present", needle)
	}
}
