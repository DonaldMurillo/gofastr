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

	results, err := auditA11yURL(srv.URL, []string{"/"})
	if err != nil {
		t.Fatalf("auditA11yURL: %v", err)
	}
	found := false
	for _, r := range results {
		for _, v := range r.Violations {
			if v.ID == "image-alt" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected an image-alt violation, got %+v", results)
	}
	report := formatAxeReport(results)
	if !strings.Contains(report, "image-alt") {
		t.Errorf("report should name the rule, got:\n%s", report)
	}
	if !strings.Contains(report, "https://") {
		t.Errorf("report should include the axe help URL, got:\n%s", report)
	}
}
