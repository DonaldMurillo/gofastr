package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// ---------------------------------------------------------------------------
// E2E tests for the head injection API (issue #5).
//
// These tests verify that ALL three tiers of head injection actually
// render in a real browser — not just in unit-test string checks.
//
// Tier 1: WithHeadHTML escape hatch (tested via website integration)
// Tier 2: Typed helpers — favicon, theme-color, description, OG, Twitter,
//         canonical URL, preconnect
// Tier 3: SEOScreen per-page override — About page implements HeadHTML()
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
// helper option configured on the website host produces real DOM elements.
// This is the E2E proof that the full typed helper tier works end-to-end.
func TestE2E_HeadInjection_AllGlobalTagsPresent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	headHTML := collectHeadHTML(t, ctx, base+"/")

	// Every typed helper configured in main.go must appear in the DOM
	checks := []struct {
		label string
		want  string
	}{
		// WithFavicon
		{"favicon", `<link rel="icon" href="/static/favicon.ico">`},
		// WithDescription
		{"description", `<meta name="description" content="GoFastr demo website — SSR framework with islands, signals, and themes">`},
		// WithThemeColor
		{"theme-color", `<meta name="theme-color" content="#f7f5ee">`},
		// WithOpenGraph (3 fields set)
		{"og:title", `<meta property="og:title" content="GoFastr">`},
		{"og:url", `<meta property="og:url" content="https://gofastr.dev">`},
		{"og:type", `<meta property="og:type" content="website">`},
		// WithTwitterCard (2 fields set)
		{"twitter:card", `<meta name="twitter:card" content="summary_large_image">`},
		{"twitter:title", `<meta name="twitter:title" content="GoFastr">`},
		// WithCanonicalURL
		{"canonical", `<link rel="canonical" href="https://gofastr.dev">`},
		// WithPreconnect (2 origins)
		{"preconnect fonts.googleapis.com", `<link rel="preconnect" href="https://fonts.googleapis.com">`},
		{"preconnect fonts.gstatic.com", `<link rel="preconnect" href="https://fonts.gstatic.com">`},
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

// TestE2E_HeadInjection_SEOScreenPerScreen verifies the per-page SEOScreen
// override. The About screen implements HeadHTML() which should inject
// page-specific og:title and og:description ALONGSIDE the global tags.
func TestE2E_HeadInjection_SEOScreenPerScreen(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	headHTML := collectHeadHTML(t, ctx, base+"/about")

	// Global tags still present on the about page
	assertContains(t, headHTML, `<link rel="icon" href="/static/favicon.ico">`)
	assertContains(t, headHTML, `<meta name="theme-color" content="#f7f5ee">`)
	assertContains(t, headHTML, `<link rel="canonical" href="https://gofastr.dev">`)

	// Per-screen SEOScreen tags from AboutScreen.HeadHTML()
	assertContains(t, headHTML, `<meta property="og:title" content="About GoFastr">`)
	assertContains(t, headHTML, `<meta property="og:description" content="Learn about the GoFastr framework, its layered design, and current status.">`)

	// Both global og:title ("GoFastr") and per-screen og:title ("About GoFastr")
	// should be present — they coexist
	ogTitleCount := strings.Count(headHTML, `<meta property="og:title"`)
	if ogTitleCount != 2 {
		t.Errorf("expected 2 og:title meta tags (1 global + 1 per-screen), got %d", ogTitleCount)
	}
}

// TestE2E_HeadInjection_NoSEOScreenOnOtherPages verifies that pages
// without SEOScreen only get the global tags — no per-screen leakage.
func TestE2E_HeadInjection_NoSEOScreenOnOtherPages(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Home page doesn't implement SEOScreen
	headHTML := collectHeadHTML(t, ctx, base+"/")

	// Global tags present
	assertContains(t, headHTML, `<link rel="icon" href="/static/favicon.ico">`)
	assertContains(t, headHTML, `<meta name="theme-color" content="#f7f5ee">`)

	// The About page's per-screen tags should NOT be on the home page
	assertNotContains(t, headHTML, `<meta property="og:title" content="About GoFastr">`)
	assertNotContains(t, headHTML, `Learn about the GoFastr framework`)

	// Only the global og:title
	ogTitleCount := strings.Count(headHTML, `<meta property="og:title"`)
	if ogTitleCount != 1 {
		t.Errorf("expected 1 og:title (global only) on home page, got %d", ogTitleCount)
	}
}

// TestE2E_HeadInjection_MultiplePagesAllGetGlobalTags verifies that
// several different pages all receive the global head tags — proving
// the injection isn't accidentally scoped to a single route.
func TestE2E_HeadInjection_MultiplePagesAllGetGlobalTags(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	for _, path := range []string{"/", "/about", "/components/accordion", "/components/tabs"} {
		path := path
		t.Run(path, func(t *testing.T) {
			headHTML := collectHeadHTML(t, ctx, base+path)
			assertContains(t, headHTML, `<link rel="icon" href="/static/favicon.ico">`)
			assertContains(t, headHTML, `<meta name="theme-color" content="#f7f5ee">`)
			assertContains(t, headHTML, `<link rel="canonical" href="https://gofastr.dev">`)
		})
	}
}

// TestE2E_HeadInjection_TitleTag verifies that the <title> element
// renders correctly in the browser's document.title — proving the
// SSR title generation works end-to-end through the full rendering
// pipeline (app.RenderPage → injectChrome → browser).
func TestE2E_HeadInjection_TitleTag(t *testing.T) {
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
	if !strings.Contains(title, "Home") || !strings.Contains(title, "GoFastr") {
		t.Errorf("home page title should contain 'Home' and 'GoFastr', got %q", title)
	}

	// Verify the about page title too
	var aboutTitle string
	err = chromedp.Run(ctx,
		chromedp.Navigate(base+"/about"),
		pageReady(),
		chromedp.Evaluate(`document.title`, &aboutTitle),
	)
	if err != nil {
		t.Fatalf("chromedp about: %v", err)
	}
	if !strings.Contains(aboutTitle, "About") {
		t.Errorf("about page title should contain 'About', got %q", aboutTitle)
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
