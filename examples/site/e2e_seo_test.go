package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
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

// hasHeadElement checks parsed attributes so page-ownership markers and attribute
// ordering do not change the assertion. Stop at </head> so a body carrier cannot
// satisfy a check for metadata in the actual document head.
func hasHeadElement(t *testing.T, body, tag string, attrs map[string]string) bool {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(body))
	inHead := false
	for {
		typ := z.Next()
		if typ == html.ErrorToken {
			if err := z.Err(); err != io.EOF {
				t.Fatalf("parse head: %v", err)
			}
			return false
		}
		token := z.Token()
		if typ == html.EndTagToken && token.Data == "head" {
			return false
		}
		if typ != html.StartTagToken && typ != html.SelfClosingTagToken {
			continue
		}
		if token.Data == "head" {
			inHead = true
		}
		if !inHead || token.Data != tag {
			continue
		}
		got := make(map[string]string, len(token.Attr))
		for _, attr := range token.Attr {
			got[attr.Key] = attr.Val
		}
		matches := true
		for key, value := range attrs {
			if actual, ok := got[key]; !ok || actual != value {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
}

func TestHeadElementMatchesAttributes(t *testing.T) {
	body := `<html><head><meta content="A &amp; B" data-cui-page-meta="" name="description"></head><body><template data-cui-page-head><link rel="canonical" href="/carrier-only"></template></body></html>`
	if !hasHeadElement(t, body, "meta", map[string]string{"name": "description", "content": "A & B"}) {
		t.Error("missed reordered, escaped head attributes")
	}
	if hasHeadElement(t, body, "meta", map[string]string{"content": "wrong description"}) {
		t.Error("accepted an incorrect attribute value")
	}
	if hasHeadElement(t, body, "link", map[string]string{"rel": "canonical", "href": "/carrier-only"}) {
		t.Error("accepted body carrier metadata as a document head element")
	}
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
		fmt.Sprintf("<loc>%s/</loc>", siteOrigin),
		// Site has /seo not /components/seo.
		fmt.Sprintf("<loc>%s/seo</loc>", siteOrigin),
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
		"Sitemap: " + siteOrigin + "/sitemap.xml",
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
	// Site route is /seo, not /components/seo.
	code, _, body := fetchE2E(t, base, "/seo")
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	if !hasHeadElement(t, body, "meta", map[string]string{"name": "description", "content": new(SEOScreen).ScreenDescription(context.Background())}) {
		t.Errorf("expected auto-emitted meta description from ScreenDescriber, got:\n%s",
			snippet(body, "meta name=\"description\""))
	}
}

func TestE2ECanonicalLink(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	// Site route is /seo; the canonical uses siteOrigin.
	_, _, body := fetchE2E(t, base, "/seo")
	if !hasHeadElement(t, body, "link", map[string]string{"rel": "canonical", "href": siteOrigin + "/seo"}) {
		t.Errorf("expected per-page canonical, got:\n%s", snippet(body, "rel=\"canonical\""))
	}
}

func TestE2EHreflangAlternates(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	_, _, body := fetchE2E(t, base, "/seo")
	for _, lang := range []string{"en", "es", "x-default"} {
		if !hasHeadElement(t, body, "link", map[string]string{"rel": "alternate", "hreflang": lang}) {
			t.Errorf("missing alternate for %q in head\n%s", lang, snippet(body, "hreflang"))
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
		chromedp.Navigate(base+"/seo"),
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
		if tp, ok := v["@type"].(string); ok {
			if _, expected := wantTypes[tp]; expected {
				wantTypes[tp] = true
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
		end := min(len(body), 400)
		return "(marker not found) " + body[:end]
	}
	start := max(i-80, 0)
	end := min(i+200, len(body))
	return body[start:end]
}

// ─── ScreenSEO bundle ────────────────────────────────────────────────

func TestE2ESEOBundleEmitsHead(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	// Site route is /seo-bundle, not /components/seo-bundle.
	// Canonical is siteOrigin + /seo-bundle.
	_, _, body := fetchE2E(t, base, "/seo-bundle")
	for _, want := range []struct {
		tag   string
		attrs map[string]string
	}{
		{"meta", map[string]string{"name": "description", "content": "Bundle-style SEO declaration in one method."}},
		{"link", map[string]string{"rel": "canonical", "href": siteOrigin + "/seo-bundle"}},
		{"link", map[string]string{"rel": "alternate", "hreflang": "en"}},
		{"link", map[string]string{"rel": "alternate", "hreflang": "es"}},
		{"meta", map[string]string{"name": "robots", "content": "index,follow"}},
		{"meta", map[string]string{"property": "og:title", "content": "GoFastr SEO bundle"}},
		{"meta", map[string]string{"property": "og:type", "content": "article"}},
		{"meta", map[string]string{"name": "twitter:card", "content": "summary_large_image"}},
		{"script", map[string]string{"type": "application/ld+json"}},
	} {
		if !hasHeadElement(t, body, want.tag, want.attrs) {
			t.Errorf("missing %s %v in bundle <head>", want.tag, want.attrs)
		}
	}
	if !strings.Contains(body, `"headline":"GoFastr SEO bundle"`) {
		t.Error("missing bundle JSON-LD headline")
	}
}
