package auditcmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sitemapFixture(documents map[string]string) func(string) (a11ySitemap, error) {
	return func(path string) (a11ySitemap, error) {
		source, ok := documents[path]
		if !ok {
			return a11ySitemap{}, fmt.Errorf("%s: %w", path, errA11ySitemapMissing)
		}
		return readA11ySitemap(source)
	}
}

func TestA11ySitemapDiscoversURLSet(t *testing.T) {
	read := sitemapFixture(map[string]string{"/sitemap.xml": `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com</loc></url>
  <url><loc>https://example.com/about</loc></url>
  <url><loc> </loc></url>
  <url><loc>https://example.com/docs/intro</loc></url>
</urlset>`})
	pages, err := discoverA11yPages(read)
	want := []string{"/", "/about", "/docs/intro"}
	if err != nil || !slices.Equal(pages, want) {
		t.Fatalf("discovered %v, error %v, want %v", pages, err, want)
	}
}

func TestA11ySitemapFallsBackToRoot(t *testing.T) {
	pages, err := discoverA11yPages(sitemapFixture(nil))
	if err != nil || !slices.Equal(pages, []string{"/"}) {
		t.Fatalf(`expected ["/"], got %v, error %v`, pages, err)
	}
}

func TestA11ySitemapRootFailuresFailDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(string) (a11ySitemap, error)
	}{
		{"unavailable", func(string) (a11ySitemap, error) {
			return a11ySitemap{}, fmt.Errorf("HTTP 503")
		}},
		{"malformed", sitemapFixture(map[string]string{"/sitemap.xml": "<urlset>"})},
		{"wrong document", sitemapFixture(map[string]string{"/sitemap.xml": "<html/>"})},
		{"empty", sitemapFixture(map[string]string{"/sitemap.xml": "<urlset/>"})},
		{"empty location", sitemapFixture(map[string]string{"/sitemap.xml": "<urlset><url><loc></loc></url></urlset>"})},
		{"blank location", sitemapFixture(map[string]string{"/sitemap.xml": "<urlset><url><loc> </loc></url></urlset>"})},
		{"blank location in part", sitemapFixture(map[string]string{
			"/sitemap.xml": "<sitemapindex><sitemap><loc>/part.xml</loc></sitemap></sitemapindex>",
			"/part.xml":    "<urlset><url><loc> </loc></url></urlset>",
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages, err := discoverA11yPages(tc.read)
			if len(pages) != 0 || err == nil || !strings.Contains(err.Error(), "/sitemap.xml") {
				t.Fatalf("discovered %v, error %v, want a failure naming the root sitemap", pages, err)
			}
		})
	}
}

func TestA11ySitemapIndexDoesNotScanPartURLs(t *testing.T) {
	read := sitemapFixture(map[string]string{
		"/sitemap.xml": `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap/1.xml</loc></sitemap>
  <sitemap><loc>https://example.com/sitemap/2.xml</loc></sitemap>
</sitemapindex>`,
		"/sitemap/1.xml": `<urlset><url><loc>https://example.com/</loc></url>
<url><loc>https://example.com/about</loc></url></urlset>`,
		"/sitemap/2.xml": `<urlset><url><loc>https://example.com/about</loc></url>
<url><loc>https://example.com/docs/intro</loc></url></urlset>`,
	})
	pages, err := discoverA11yPages(read)
	if err != nil || !slices.Equal(pages, []string{"/", "/about", "/docs/intro"}) {
		t.Fatalf("got %v, error %v, want unique application pages from both parts", pages, err)
	}
}

func TestA11ySitemapMissingPartFailsDiscovery(t *testing.T) {
	read := sitemapFixture(map[string]string{
		"/sitemap.xml": `<sitemapindex><sitemap><loc>/sitemap/1.xml</loc></sitemap></sitemapindex>`,
	})
	pages, err := discoverA11yPages(read)
	if len(pages) != 0 || err == nil || !strings.Contains(err.Error(), "/sitemap/1.xml") {
		t.Fatalf("discovered %v, error %v, want a failure naming the missing part", pages, err)
	}
}

// TestAuditA11yURLFindsImageAltViolation drives the real headless-Chrome
// + axe-core path against a deliberately broken page. Needs Chrome.
func TestAuditA11yURLFindsImageAltViolation(t *testing.T) {
	if testing.Short() {
		t.Skip("headless-Chrome axe scan skipped in -short")
	}
	// The scan records coverage in the axe manifest, which defaults to the
	// module root: this repo's own .gofastr/. A one-page manifest there
	// makes every example's strict boot panic in the dev-loop e2e.
	covDir := t.TempDir()
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", covDir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<sitemapindex><sitemap><loc>/sitemap/1.xml</loc></sitemap></sitemapindex>`)
			return
		case "/sitemap/1.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<urlset><url><loc>/broken</loc></url></urlset>`)
			return
		case "/broken":
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html lang="en"><head><title>Broken</title></head><body>
<main><h1>Broken page</h1>
<p>one</p><p>two</p><p>three</p><p>four</p>
<img src="data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==">
</main></body></html>`))
	}))
	defer srv.Close()

	run, err := auditA11yURL(srv.URL, nil, a11yCredentials{})
	if err != nil {
		t.Fatalf("auditA11yURL: %v", err)
	}
	if !slices.Equal(run.Pages, []string{"/broken"}) {
		t.Fatalf("audit discovered %v, want [/broken]", run.Pages)
	}
	found := false
	for _, r := range run.Results {
		for _, v := range r.Violations {
			if v.ID == "image-alt" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected an image-alt violation, got %+v", run.Results)
	}
	report := formatAxeReport(run)
	if !strings.Contains(report, "image-alt") {
		t.Errorf("report should name the rule, got:\n%s", report)
	}
	if !strings.Contains(report, "https://") {
		t.Errorf("report should include the axe help URL, got:\n%s", report)
	}
	if _, err := os.Stat(filepath.Join(covDir, ".gofastr", "axe-coverage.json")); err != nil {
		t.Errorf("coverage manifest not written under the test's dir: %v", err)
	}
}

func TestAxeReportShowsCoverage(t *testing.T) {
	run := axeAuditRun{
		Pages: []string{"/admin", "/settings"},
		Unreachable: []a11yUnreachable{{
			Page: "/settings", Reason: "redirected to /login",
		}},
	}
	report := formatAxeReport(run)
	if !strings.Contains(report, "Audited 1 of 2 discovered pages.") {
		t.Fatalf("missing coverage: %s", report)
	}
	if !strings.Contains(report, "/settings (redirected to /login)") {
		t.Fatalf("missing unreachable page: %s", report)
	}
	if strings.Contains(report, "Both color schemes are clean") {
		t.Fatalf("incomplete run claimed clean: %s", report)
	}
}

func TestAxeReportFlagsLoginOnly(t *testing.T) {
	report := formatAxeReport(axeAuditRun{Pages: []string{"/login"}})
	if !strings.Contains(report, "only /login was audited") {
		t.Fatalf("missing login-only warning: %s", report)
	}
	if !strings.Contains(report, "reachable") {
		t.Fatalf("login-only run claimed clean: %s", report)
	}
}
