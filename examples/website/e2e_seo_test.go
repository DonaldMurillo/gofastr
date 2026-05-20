package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// fetchE2E hits the e2e server with the standard net/http client. Used
// for endpoints that aren't real pages (sitemap.xml, robots.txt) where
// spinning up a full browser is overkill.
func fetchE2E(t *testing.T, base, path string) (int, http.Header, string) {
	t.Helper()
	res, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

func TestE2ESitemapXML(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	code, hdr, body := fetchE2E(t, base, "/sitemap.xml")
	if code != 200 {
		t.Fatalf("expected 200, got %d\n%s", code, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("expected xml content type, got %q", ct)
	}
	for _, want := range []string{
		`<?xml version="1.0"`,
		`xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"`,
		`<loc>https://gofastr.dev/</loc>`,
		`<loc>https://gofastr.dev/components/seo</loc>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %s\n%s", want, body)
		}
	}
}

func TestE2ERobotsTxt(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	code, hdr, body := fetchE2E(t, base, "/robots.txt")
	if code != 200 {
		t.Fatalf("expected 200, got %d\n%s", code, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content type, got %q", ct)
	}
	for _, want := range []string{
		"User-agent: *",
		"Disallow: /__gofastr/",
		"Sitemap: https://gofastr.dev/sitemap.xml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("robots.txt missing %q\n%s", want, body)
		}
	}
}

func TestE2EAutoMetaDescription(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	base = base
	code, _, body := fetchE2E(t, base, "/components/seo")
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	if !strings.Contains(body, `<meta name="description" content="Per-page SEO demo`) {
		t.Errorf("expected auto-emitted meta description from ScreenDescriber, got:\n%s",
			snippet(body, "meta name=\"description\""))
	}
}

func TestE2ECanonicalLink(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	_, _, body := fetchE2E(t, base, "/components/seo")
	want := `<link rel="canonical" href="https://example.com/components/seo">`
	if !strings.Contains(body, want) {
		t.Errorf("expected per-page canonical, got:\n%s", snippet(body, "rel=\"canonical\""))
	}
}

func TestE2EHreflangAlternates(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	_, _, body := fetchE2E(t, base, "/components/seo")
	for _, want := range []string{
		`<link rel="alternate" hreflang="en"`,
		`<link rel="alternate" hreflang="es"`,
		`<link rel="alternate" hreflang="x-default"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in head\n%s", want, snippet(body, "hreflang"))
		}
	}
}

func TestE2EJSONLDArticleAndBreadcrumb(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/seo"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify(
			Array.from(document.querySelectorAll('script[type="application/ld+json"]'))
				.map(s => s.textContent)
		)`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var scripts []string
	if err := json.Unmarshal([]byte(raw), &scripts); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(scripts) < 2 {
		t.Fatalf("expected ≥2 JSON-LD scripts (Article + BreadcrumbList), got %d", len(scripts))
	}
	// Each script should be valid JSON with the right @type.
	wantTypes := map[string]bool{"Article": false, "BreadcrumbList": false}
	for _, s := range scripts {
		// Un-escape "</" that the renderer escaped to "<\/".
		clean := strings.ReplaceAll(s, `<\/`, "</")
		var v map[string]any
		if err := json.Unmarshal([]byte(clean), &v); err != nil {
			t.Errorf("JSON-LD payload not parseable: %v\n%s", err, clean)
			continue
		}
		if t, ok := v["@type"].(string); ok {
			if _, expected := wantTypes[t]; expected {
				wantTypes[t] = true
			}
		}
	}
	for typ, found := range wantTypes {
		if !found {
			t.Errorf("expected JSON-LD @type %q in head", typ)
		}
	}
}

// snippet returns a chunk of body around the marker for error messages
// so failures don't dump the whole 30KB page.
func snippet(body, marker string) string {
	i := strings.Index(body, marker)
	if i < 0 {
		end := len(body)
		if end > 400 {
			end = 400
		}
		return "(marker not found) " + body[:end]
	}
	start := i - 80
	if start < 0 {
		start = 0
	}
	end := i + 200
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}
