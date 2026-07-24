package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type productPagesComp struct{ slug string }

func (p *productPagesComp) Render() render.HTML {
	return html.Div(html.DivConfig{}, render.Text("product "+p.slug))
}
func (p *productPagesComp) SetParams(params map[string]string) {
	p.slug = params["slug"]
}
func (p *productPagesComp) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"slug": "alpha"}, {"slug": "beta"}}
}

type plainComp struct{}

func (plainComp) Render() render.HTML         { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (plainComp) SetParams(map[string]string) {}

func TestSitemap404WithoutOption(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Errorf("sitemap.xml should not be served without WithSitemap, got 200")
	}
}

func TestSitemapXMLBasic(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	a.Register("/about", &plainComp{}, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}))
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("expected XML preamble, got:\n%s", body)
	}
	if !strings.Contains(body, `xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"`) {
		t.Errorf("expected sitemap namespace, got:\n%s", body)
	}
	if !strings.Contains(body, `<loc>https://example.com/</loc>`) {
		t.Errorf("expected root loc, got:\n%s", body)
	}
	if !strings.Contains(body, `<loc>https://example.com/about</loc>`) {
		t.Errorf("expected /about loc, got:\n%s", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("expected xml content type, got %q", ct)
	}
}

func TestSitemapExpandsDynamicRoutes(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	a.Register("/products/:slug", &productPagesComp{}, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}))
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, ":slug") {
		t.Errorf("dynamic placeholder must be expanded, got: %s", body)
	}
	if !strings.Contains(body, "/products/alpha") || !strings.Contains(body, "/products/beta") {
		t.Errorf("expected expanded slugs, got: %s", body)
	}
}

func TestSitemapSkipsDynamicWithoutStaticPaths(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	a.Register("/u/:id", &plainComp{}, nil) // no StaticPathsProvider
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}))
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, ":id") || strings.Contains(body, "/u/") {
		t.Errorf("expected /u/:id to be skipped, got: %s", body)
	}
}

func TestSitemapExcludePaths(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	a.Register("/admin", &plainComp{}, nil)
	a.Register("/admin/users", &plainComp{}, nil)
	a.Register("/about", &plainComp{}, nil)
	ds := New(a, WithSitemap(SitemapConfig{
		BaseURL:      "https://example.com",
		ExcludePaths: []string{"/admin"},
	}))
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "/admin") {
		t.Errorf("expected /admin to be excluded, got: %s", body)
	}
	if !strings.Contains(body, "/about") {
		t.Errorf("expected /about to remain, got: %s", body)
	}
}

func TestRobotsDefaultsToAllow(t *testing.T) {
	ds := New(app.NewApp("x"), WithRobots(RobotsConfig{}))
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "User-agent: *") {
		t.Errorf("expected default User-agent *, got:\n%s", body)
	}
	if !strings.Contains(body, "Allow: /") {
		t.Errorf("expected default Allow: /, got:\n%s", body)
	}
}

func TestRobotsCustomRules(t *testing.T) {
	ds := New(app.NewApp("x"), WithRobots(RobotsConfig{
		UserAgent:  "GPTBot",
		Disallow:   []string{"/private", "/admin"},
		CrawlDelay: 5,
		SitemapURL: "https://example.com/sitemap.xml",
	}))
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{
		"User-agent: GPTBot",
		"Disallow: /private",
		"Disallow: /admin",
		"Crawl-delay: 5",
		"Sitemap: https://example.com/sitemap.xml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in robots.txt, got:\n%s", want, body)
		}
	}
}

func TestRobotsDerivesSitemapFromBaseURL(t *testing.T) {
	// When WithSitemap is configured and RobotsConfig.SitemapURL is
	// empty, robots.txt should reference the BaseURL-derived sitemap.
	ds := New(app.NewApp("x"),
		WithSitemap(SitemapConfig{BaseURL: "https://example.com"}),
		WithRobots(RobotsConfig{}),
	)
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("expected derived Sitemap line, got:\n%s", body)
	}
}

func TestRobots404WithoutOption(t *testing.T) {
	ds := New(app.NewApp("x"))
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Errorf("robots.txt should not be served without WithRobots, got 200")
	}
}

// ─── Hreflang / Canonical / Schema ─────────────────────────────────

type localizedComp struct{}

func (localizedComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (localizedComp) ScreenHreflangs() []HreflangLink {
	return []HreflangLink{
		{Lang: "en", URL: "https://example.com/en/about"},
		{Lang: "es", URL: "https://example.com/es/about"},
		{Lang: "x-default", URL: "https://example.com/about"},
	}
}

func TestHreflangEmitsAlternateLinks(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/about", &localizedComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{
		`<link rel="alternate" hreflang="en" href="https://example.com/en/about">`,
		`<link rel="alternate" hreflang="es" href="https://example.com/es/about">`,
		`<link rel="alternate" hreflang="x-default" href="https://example.com/about">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s in head, got:\n%s", want, body)
		}
	}
}

type canonicalComp struct{}

func (canonicalComp) Render() render.HTML     { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (canonicalComp) ScreenCanonical() string { return "https://example.com/canonical-path" }

func TestCanonicalLink(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &canonicalComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	want := `<link rel="canonical" href="https://example.com/canonical-path">`
	if !strings.Contains(body, want) {
		t.Errorf("expected canonical link, got:\n%s", body)
	}
}

type articleComp struct{}

func (articleComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("post")) }
func (articleComp) ScreenSchema() []seo.Thing {
	a := seo.NewArticle()
	a.Headline = "Hello World"
	a.URL = "https://example.com/posts/hello"
	return []seo.Thing{a}
}

func TestScreenSchemaEmitsJSONLD(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/post", &articleComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/post", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `<script type="application/ld+json">`) {
		t.Errorf("expected JSON-LD script, got:\n%s", body)
	}
	if !strings.Contains(body, `"headline":"Hello World"`) {
		t.Errorf("expected headline in JSON-LD, got:\n%s", body)
	}
}

// ─── ScreenSEO bundle ──────────────────────────────────────────────

type bundleComp struct{}

func (bundleComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (bundleComp) ScreenSEO() SEO {
	return SEO{
		Description: "Bundle desc",
		Canonical:   "https://example.com/c",
		Hreflangs:   []HreflangLink{{Lang: "fr", URL: "https://example.com/fr"}},
		Robots:      "noindex",
		OG:          &OG{Title: "OGT", Image: "/og.png"},
		Twitter:     &TwitterCard{Card: "summary", Title: "TwT"},
		Schema:      []seo.Thing{seo.NewArticle()},
	}
}

func TestSEOBundleEmitsAllTags(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &bundleComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{
		`<meta name="description" content="Bundle desc">`,
		`<link rel="canonical" href="https://example.com/c">`,
		`<link rel="alternate" hreflang="fr" href="https://example.com/fr">`,
		`<meta name="robots" content="noindex">`,
		`<meta property="og:title" content="OGT">`,
		`<meta property="og:image" content="/og.png">`,
		`<meta name="twitter:card" content="summary">`,
		`<meta name="twitter:title" content="TwT">`,
		`<script type="application/ld+json">`,
		`"@type":"Article"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in <head>", want)
		}
	}
}

// Bundle's Description should win over the screen.Description set by
// ScreenDescriber when both are present.
type bundleAndDescriberComp struct{}

func (bundleAndDescriberComp) Render() render.HTML {
	return html.Div(html.DivConfig{}, render.Text("hi"))
}
func (bundleAndDescriberComp) ScreenDescription() string { return "From describer" }
func (bundleAndDescriberComp) ScreenSEO() SEO            { return SEO{Description: "From bundle"} }

func TestSEOBundleDescriptionOverrides(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &bundleAndDescriberComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `content="From bundle"`) {
		t.Errorf("expected bundle description to win, got:\n%s", body)
	}
	if strings.Contains(body, `content="From describer"`) {
		t.Errorf("bundle description should override ScreenDescriber, got:\n%s", body)
	}
}

// ─── F13: per-page OG beats global OG for first-match crawlers ──────

// ogOverrideComp returns a per-page og:title via ScreenSEO.
type ogOverrideComp struct{}

func (ogOverrideComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (ogOverrideComp) ScreenSEO() SEO {
	return SEO{OG: &OG{Title: "Per-page OG title"}}
}

// TestPerPageOGBeatsGlobal asserts that when both global WithOpenGraph and
// per-page ScreenSEO set og:title, the per-page tag appears FIRST in <head>
// so first-match crawlers honour the per-page value.
func TestPerPageOGBeatsGlobal(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/product", &ogOverrideComp{}, nil)
	ds := New(a, WithOpenGraph(OG{Title: "Global site title"}))
	req := httptest.NewRequest("GET", "/product", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()

	// Both tags must be present.
	if !strings.Contains(body, `content="Per-page OG title"`) {
		t.Fatalf("per-page og:title missing from page:\n%s", body)
	}
	if !strings.Contains(body, `content="Global site title"`) {
		t.Fatalf("global og:title missing from page:\n%s", body)
	}

	// Per-page must come first: first-match crawlers honour the earlier tag.
	perPageIdx := strings.Index(body, `content="Per-page OG title"`)
	globalIdx := strings.Index(body, `content="Global site title"`)
	if perPageIdx > globalIdx {
		t.Errorf("global og:title (pos %d) appears before per-page og:title (pos %d); first-match crawlers will ignore the per-page override",
			globalIdx, perPageIdx)
	}
}

// Empty bundle fields fall through to per-concern interfaces.
type partialBundleComp struct{}

func (partialBundleComp) Render() render.HTML       { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (partialBundleComp) ScreenDescription() string { return "fallback desc" }
func (partialBundleComp) ScreenCanonical() string   { return "https://example.com/c-fallback" }
func (partialBundleComp) ScreenSEO() SEO            { return SEO{Robots: "noindex"} } // only robots in bundle

func TestSEOBundleFallsThroughEmptyFields(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &partialBundleComp{}, nil)
	ds := New(a)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{
		`content="fallback desc"`,               // from ScreenDescriber
		`href="https://example.com/c-fallback"`, // from ScreenCanonical
		`content="noindex"`,                     // from bundle
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in <head>", want)
		}
	}
}
