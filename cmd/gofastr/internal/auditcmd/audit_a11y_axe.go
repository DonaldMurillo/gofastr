package auditcmd

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
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

func (r axeAuditRun) Incomplete() bool { return len(r.Unreachable) > 0 || r.onlyLogin() }

type a11ySitemap struct {
	XMLName xml.Name
	Pages   []string `xml:"url>loc"`
	Parts   []string `xml:"sitemap>loc"`
}

var errA11ySitemapMissing = errors.New("sitemap not found")

func discoverA11yPages(readSitemap func(path string) (a11ySitemap, error)) ([]string, error) {
	root, err := readSitemap("/sitemap.xml")
	if errors.Is(err, errA11ySitemapMissing) {
		return []string{"/"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sitemap /sitemap.xml: %w", err)
	}
	var locations []string
	switch root.XMLName.Local {
	case "urlset":
		locations = root.Pages
	case "sitemapindex":
		seenParts := map[string]bool{}
		for _, loc := range root.Parts {
			part, err := url.Parse(strings.TrimSpace(loc))
			if err != nil || !strings.HasPrefix(part.Path, "/") {
				return nil, fmt.Errorf("invalid sitemap part location %q", loc)
			}
			path := part.RequestURI()
			if seenParts[path] {
				continue
			}
			seenParts[path] = true
			doc, err := readSitemap(path)
			if err != nil {
				return nil, fmt.Errorf("sitemap part %s: %w", path, err)
			}
			if doc.XMLName.Local != "urlset" {
				return nil, fmt.Errorf("sitemap part %s: expected a URL set, got %s", path, doc.XMLName.Local)
			}
			locations = append(locations, doc.Pages...)
		}
	default:
		return nil, fmt.Errorf("sitemap /sitemap.xml: expected a URL set or sitemap index, got %q", root.XMLName.Local)
	}
	var pages []string
	seen := map[string]bool{}
	for _, loc := range locations {
		loc = strings.TrimSpace(loc)
		u, err := url.Parse(loc)
		if err != nil {
			continue
		}
		switch u.Scheme {
		case "http", "https":
			if u.Hostname() == "" {
				continue
			}
		case "":
			if !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//") {
				continue
			}
		default:
			continue
		}
		p := u.EscapedPath()
		if p == "" {
			p = "/"
		}
		if !seen[p] {
			seen[p] = true
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("sitemap /sitemap.xml: no page URLs found")
	}
	return pages, nil
}

func readA11ySitemap(source string) (a11ySitemap, error) {
	var doc a11ySitemap
	err := xml.Unmarshal([]byte(source), &doc)
	return doc, err
}

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
		pages, err = discoverA11yPagesFromBrowser(browser, base)
		if err != nil {
			return axeAuditRun{}, err
		}
	}
	cli.Printf("Auditing %d page(s) at %s under %d color scheme(s)…\n\n", len(pages), base, len(axetest.Schemes))

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
		chromedp.Click(`button[type="submit"], input[type="submit"]`, chromedp.ByQuery),
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

func discoverA11yPagesFromBrowser(browser context.Context, base string) ([]string, error) {
	return discoverA11yPages(func(path string) (a11ySitemap, error) {
		ctx, cancel := axetest.NewTabContext(browser, 15*time.Second)
		defer cancel()
		var source string
		// Chrome's XML viewer moves the original document under its source element.
		resp, err := chromedp.RunResponse(ctx,
			chromedp.Navigate(base+path),
			chromedp.Evaluate(`new XMLSerializer().serializeToString(
				document.getElementById('webkit-xml-viewer-source-xml')?.firstElementChild ?? document.documentElement
			)`, &source),
		)
		if err != nil {
			return a11ySitemap{}, err
		}
		if resp == nil {
			return a11ySitemap{}, fmt.Errorf("sitemap request returned no response")
		}
		if !samePage(base+path, resp.URL) {
			return a11ySitemap{}, fmt.Errorf("sitemap request did not return a document at the requested path (HTTP %d)", resp.Status)
		}
		switch resp.Status {
		case http.StatusNotFound, http.StatusGone:
			return a11ySitemap{}, fmt.Errorf("HTTP %d: %w", resp.Status, errA11ySitemapMissing)
		case http.StatusOK:
		default:
			return a11ySitemap{}, fmt.Errorf("sitemap request returned HTTP %d", resp.Status)
		}
		return readA11ySitemap(source)
	})
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
	fmt.Fprintf(&b, "Accessibility audit: %d violation(s)\n\n", total)
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
