package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/axetest"
	"github.com/chromedp/chromedp"
)

// AxePageResult is one page × color-scheme axe run.
type AxePageResult struct {
	Page       string
	Scheme     string
	Violations []axetest.Violation
}

// discoverA11yPages returns the route paths to audit for base: every
// <loc> in the app's /sitemap.xml (the WithSitemap endpoint — also
// written by static export), falling back to just "/" when no sitemap
// is served. Explicit --pages always bypasses discovery.
func discoverA11yPages(base string) []string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(strings.TrimRight(base, "/") + "/sitemap.xml")
	if err != nil {
		return []string{"/"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return []string{"/"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return []string{"/"}
	}
	locs := sitemapLocRE.FindAllStringSubmatch(string(body), -1)
	var pages []string
	seen := map[string]bool{}
	for _, m := range locs {
		u, err := url.Parse(strings.TrimSpace(m[1]))
		if err != nil {
			continue
		}
		p := u.Path
		if p == "" {
			p = "/"
		}
		if !seen[p] {
			seen[p] = true
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return []string{"/"}
	}
	return pages
}

var sitemapLocRE = regexp.MustCompile(`<loc>\s*([^<]+?)\s*</loc>`)

// auditA11yURL runs the vendored axe-core engine against every page
// under both color schemes via headless Chrome — the same harness the
// framework's own a11y gates use. The caller controls the page list;
// pass nil to audit just "/".
func auditA11yURL(base string, pages []string) ([]AxePageResult, error) {
	if len(pages) == 0 {
		pages = []string{"/"}
	}
	base = strings.TrimRight(base, "/")
	browser, cancel := axetest.NewBrowserContext(context.Background())
	defer cancel()

	var results []AxePageResult
	for _, page := range pages {
		for _, scheme := range axetest.Schemes {
			vs, err := axeScanPage(browser, base, page, scheme)
			if err != nil {
				return results, fmt.Errorf("axe on %s (%s): %w", page, scheme, err)
			}
			if len(vs) > 0 {
				results = append(results, AxePageResult{Page: page, Scheme: scheme, Violations: vs})
			}
		}
	}
	return results, nil
}

func axeScanPage(browser context.Context, base, page, scheme string) ([]axetest.Violation, error) {
	ctx, cancel := axetest.NewTabContext(browser, 30*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+page),
		chromedp.Sleep(400*time.Millisecond), // hydration settle
		axetest.Prepare(scheme),
	); err != nil {
		return nil, err
	}
	return axetest.Scan(ctx, scheme, nil)
}

// formatAxeReport renders the runtime audit: one block per page×scheme,
// each violation with impact, help text, the axe help URL, and the DOM
// targets — enough to find and fix the element without re-running.
func formatAxeReport(results []AxePageResult) string {
	total := 0
	for _, r := range results {
		total += len(r.Violations)
	}
	if total == 0 {
		return "No accessibility violations found. Both color schemes are clean.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Accessibility audit — %d violation(s)\n\n", total)
	for _, r := range results {
		fmt.Fprintf(&b, "%s (%s scheme)\n", r.Page, r.Scheme)
		for _, v := range r.Violations {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", v.Impact, v.ID, v.Help)
			fmt.Fprintf(&b, "      guide: %s\n", v.HelpURL)
			for _, n := range v.Nodes {
				fmt.Fprintf(&b, "      at: %s\n", strings.Join(n.Target, " "))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
