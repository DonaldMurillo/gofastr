package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// SEODemoScreen demonstrates every per-page SEO interface in one place:
// ScreenDescriber → <meta name="description">, ScreenCanonical →
// <link rel="canonical">, ScreenHreflangs → <link rel="alternate">,
// ScreenSchema → <script type="application/ld+json">.
type SEODemoScreen struct{}

func (*SEODemoScreen) ScreenTitle() string {
	return "SEO"
}

// ScreenDescription auto-emits a per-page meta description. This is
// the simplest SEO surface and the one most sites forget.
func (*SEODemoScreen) ScreenDescription() string {
	return "Per-page SEO demo: canonical URL, hreflang alternates, JSON-LD article schema, plus sitewide sitemap.xml and robots.txt."
}

func (*SEODemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// ScreenCanonical reports the canonical URL for this page so duplicate
// query-string variants don't fragment search-engine ranking.
func (*SEODemoScreen) ScreenCanonical() string {
	return "https://example.com/components/seo"
}

// ScreenHreflangs declares locale alternates. The "x-default" entry is
// the fallback the search engine serves when no locale matches.
func (*SEODemoScreen) ScreenHreflangs() []uihost.HreflangLink {
	return []uihost.HreflangLink{
		{Lang: "en", URL: "https://example.com/components/seo"},
		{Lang: "es", URL: "https://example.com/es/components/seo"},
		{Lang: "x-default", URL: "https://example.com/components/seo"},
	}
}

// ScreenSchema declares typed Schema.org items rendered as
// <script type="application/ld+json">. Multiple items are allowed —
// they each emit their own script tag.
func (*SEODemoScreen) ScreenSchema() []seo.Thing {
	article := seo.NewArticle()
	article.Headline = "GoFastr SEO module"
	article.Description = "How per-page SEO surfaces compose in GoFastr."
	article.URL = "https://example.com/components/seo"
	article.DatePublished = "2026-05-19"

	bc := seo.NewBreadcrumbList(
		seo.BreadcrumbItem{Name: "Home", URL: "https://example.com/"},
		seo.BreadcrumbItem{Name: "Components", URL: "https://example.com/components/"},
		seo.BreadcrumbItem{Name: "SEO", URL: "https://example.com/components/seo"},
	)
	return []seo.Thing{article, bc}
}

func (s *SEODemoScreen) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("SEO")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Per-page SEO is wired through four small interfaces — implement the ones you need, skip the rest. The uihost auto-emits the right tags in <head>.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Per-page interfaces")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text(
				"app.ScreenDescriber — ScreenDescription() string → <meta name=\"description\">. This screen returns the lede paragraph; view-source to see it in <head>.")),
			render.Tag("li", nil, render.Text(
				"uihost.ScreenCanonical — ScreenCanonical() string → <link rel=\"canonical\">. Stops duplicate-content fragmentation from query-string variants.")),
			render.Tag("li", nil, render.Text(
				"uihost.ScreenHreflangs — ScreenHreflangs() []HreflangLink → one <link rel=\"alternate\"> per locale. Required for Google to serve the right language variant.")),
			render.Tag("li", nil,
				render.Text("uihost.ScreenSchema — ScreenSchema() []seo.Thing → one "),
				render.Tag("code", nil, render.Text(`<script type="application/ld+json">`)),
				render.Text(" per item. This page emits an Article + a BreadcrumbList.")),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sitewide endpoints")),
		render.Tag("p", nil, render.Text(
			"Two host options register sitewide endpoints — both gated on opt-in so apps that don't want them aren't accidentally indexed:")),
		render.Tag("ul", nil,
			render.Tag("li", nil,
				render.Tag("a", map[string]string{"href": "/sitemap.xml"}, render.Text("/sitemap.xml")),
				render.Text(" — uihost.WithSitemap(SitemapConfig{BaseURL: …}). Lists every reachable route; dynamic routes are expanded via StaticPathsProvider.")),
			render.Tag("li", nil,
				render.Tag("a", map[string]string{"href": "/robots.txt"}, render.Text("/robots.txt")),
				render.Text(" — uihost.WithRobots(RobotsConfig{…}). With nil-zero config ships the open default (Allow: /). Auto-references the sitemap when both are configured.")),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("What's emitted on this page")),
		render.Tag("p", nil, render.Text(
			"View-source on this page or open DevTools to see the auto-emitted <head> tags. The JSON-LD script is data, not code — strict CSP (default-src 'self') accepts it via the application/ld+json type.")),
	)
}
