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

type a11yCredentials struct {
	Email    string
	Password string
}

type a11yUnreachable struct {
	Page   string
	Reason string
}

// axeAuditRun carries both findings and coverage. Coverage is kept separate
// from violations so a login redirect can never masquerade as a clean page.
type axeAuditRun struct {
	Pages       []string
	Results     []AxePageResult
	Unreachable []a11yUnreachable
}

func (r axeAuditRun) auditedPages() int {
	return len(r.Pages) - len(r.Unreachable)
}

func (r axeAuditRun) onlyLogin() bool {
	return len(r.Pages) == 1 && cleanPagePath(r.Pages[0]) == "/login" && len(r.Unreachable) == 0
}

// Incomplete reports coverage that must not pass CI as a clean audit.
func (r axeAuditRun) Incomplete() bool { return len(r.Unreachable) > 0 || r.onlyLogin() }

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
	return a11yPagesFromSitemap(string(body))
}

func a11yPagesFromSitemap(body string) []string {
	locs := sitemapLocRE.FindAllStringSubmatch(body, -1)
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
func auditA11yURL(base string, pages []string, credentials a11yCredentials) (axeAuditRun, error) {
	base = strings.TrimRight(base, "/")
	browser, cancel, err := axetest.NewBrowserContext(context.Background())
	if err != nil {
		return axeAuditRun{}, err
	}
	defer cancel()
	if credentials.Email != "" {
		if err := loginA11y(browser, base, credentials); err != nil {
			return axeAuditRun{}, err
		}
	}
	if len(pages) == 0 {
		pages = discoverA11yPagesFromBrowser(browser, base)
	}
	if len(pages) == 0 {
		pages = []string{"/"}
	}
	fmt.Printf("Auditing %d page(s) at %s under %d color scheme(s)…\n\n", len(pages), base, len(axetest.Schemes))

	run := axeAuditRun{Pages: append([]string(nil), pages...)}
	for _, page := range pages {
		for _, scheme := range axetest.Schemes {
			vs, finalURL, err := axeScanPage(browser, base, page, scheme)
			if err != nil {
				return run, fmt.Errorf("axe on %s (%s): %w", page, scheme, err)
			}
			if !samePage(page, finalURL) {
				run.Unreachable = append(run.Unreachable, a11yUnreachable{
					Page: page, Reason: "redirected to " + cleanPagePath(finalURL),
				})
				break
			}
			if len(vs) > 0 {
				run.Results = append(run.Results, AxePageResult{Page: page, Scheme: scheme, Violations: vs})
			}
		}
	}
	return run, nil
}

func loginA11y(browser context.Context, base string, credentials a11yCredentials) error {
	ctx, cancel := axetest.NewTabContext(browser, 30*time.Second)
	defer cancel()
	var finalURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/login"),
		chromedp.WaitVisible(`input[name="email"]`, chromedp.ByQuery),
		chromedp.SetValue(`input[name="email"]`, credentials.Email, chromedp.ByQuery),
		chromedp.SetValue(`input[name="password"]`, credentials.Password, chromedp.ByQuery),
		// Submit does not dispatch the form's submit event. The app's real
		// button is the user path and preserves cookie + redirect behavior.
		chromedp.Click(`button[type="submit"], input[type="submit"]`, chromedp.ByQuery),
		// A JS poll is tied to the pre-submit execution context and fails as
		// soon as the successful redirect replaces that context. Let the normal
		// navigation settle, then inspect the destination URL.
		chromedp.Sleep(750*time.Millisecond),
		chromedp.Location(&finalURL),
	); err != nil {
		return fmt.Errorf("login through /login: %w", err)
	}
	if cleanPagePath(finalURL) == "/login" {
		return fmt.Errorf("login through /login did not authenticate")
	}
	return nil
}

func discoverA11yPagesFromBrowser(browser context.Context, base string) []string {
	ctx, cancel := axetest.NewTabContext(browser, 15*time.Second)
	defer cancel()
	var source string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/sitemap.xml"),
		chromedp.Evaluate(`document.documentElement.outerHTML`, &source),
	); err != nil {
		return []string{"/"}
	}
	return a11yPagesFromSitemap(source)
}

func axeScanPage(browser context.Context, base, page, scheme string) ([]axetest.Violation, string, error) {
	ctx, cancel := axetest.NewTabContext(browser, 30*time.Second)
	defer cancel()
	var finalURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+page),
		chromedp.Sleep(400*time.Millisecond), // hydration settle
		chromedp.Location(&finalURL),
		axetest.Prepare(scheme),
	); err != nil {
		return nil, finalURL, err
	}
	vs, err := axetest.Scan(ctx, scheme, nil)
	return vs, finalURL, err
}

func samePage(want, got string) bool { return cleanPagePath(want) == cleanPagePath(got) }

func cleanPagePath(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Path != "" {
		raw = u.Path
	}
	if raw == "" {
		return "/"
	}
	if raw != "/" {
		raw = strings.TrimRight(raw, "/")
	}
	return raw
}

// formatAxeReport renders the runtime audit: one block per page×scheme,
// each violation with impact, help text, the axe help URL, and the DOM
// targets — enough to find and fix the element without re-running.
func formatAxeReport(run axeAuditRun) string {
	total := 0
	for _, r := range run.Results {
		total += len(r.Violations)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Audited %d of %d discovered pages.\n", run.auditedPages(), len(run.Pages))
	if len(run.Unreachable) > 0 {
		b.WriteString("Could not reach:\n")
		for _, page := range run.Unreachable {
			fmt.Fprintf(&b, "  %s (%s)\n", page.Page, page.Reason)
		}
	}
	if run.onlyLogin() {
		b.WriteString("Coverage warning: only /login was audited; authenticated pages may be missing.\n")
	}
	b.WriteString("\n")
	if total == 0 {
		if run.Incomplete() {
			b.WriteString("No accessibility violations found on the pages that were reachable.\n")
		} else {
			b.WriteString("No accessibility violations found. Both color schemes are clean.\n")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "Accessibility audit — %d violation(s)\n\n", total)
	for _, r := range run.Results {
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
