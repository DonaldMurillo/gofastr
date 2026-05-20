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

func (plainComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("hi")) }

func TestSitemapLists404WithoutOption(t *testing.T) {
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

func (canonicalComp) Render() render.HTML        { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (canonicalComp) ScreenCanonical() string    { return "https://example.com/canonical-path" }

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
