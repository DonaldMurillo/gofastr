package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverPagesFromSitemap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sitemap.xml" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
  <url><loc>https://example.com/about</loc></url>
  <url><loc>https://example.com/docs/intro</loc></url>
</urlset>`))
	}))
	defer srv.Close()

	pages := discoverA11yPages(srv.URL)
	want := []string{"/", "/about", "/docs/intro"}
	if len(pages) != len(want) {
		t.Fatalf("expected %v, got %v", want, pages)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Errorf("page %d: want %q, got %q", i, want[i], pages[i])
		}
	}
}

func TestDiscoverPagesFallsBackToRoot(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	pages := discoverA11yPages(srv.URL)
	if len(pages) != 1 || pages[0] != "/" {
		t.Errorf(`expected ["/"], got %v`, pages)
	}
}

// TestAuditA11yURLFindsImageAltViolation drives the real headless-Chrome
// + axe-core path against a deliberately broken page. Needs Chrome.
func TestAuditA11yURLFindsImageAltViolation(t *testing.T) {
	if testing.Short() {
		t.Skip("headless-Chrome axe scan skipped in -short")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html lang="en"><head><title>Broken</title></head><body>
<main><h1>Broken page</h1>
<p>one</p><p>two</p><p>three</p><p>four</p>
<img src="data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==">
</main></body></html>`))
	}))
	defer srv.Close()

	run, err := auditA11yURL(srv.URL, []string{"/"}, a11yCredentials{})
	if err != nil {
		t.Fatalf("auditA11yURL: %v", err)
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
