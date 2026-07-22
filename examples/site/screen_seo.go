package main

// =============================================================================
// /seo + /seo-bundle — demonstrates every per-page SEO surface the framework
// exposes. Ported from examples/website. The value is in the interface
// implementations below (ScreenCanonical / ScreenHreflangs / ScreenSchema /
// ScreenSEO); the uihost auto-emits the matching <head> tags. View-source on
// the live page to see them. Sitemap + robots are wired in main.go's host.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// SEOScreen implements the four per-concern SEO interfaces individually.
type SEOScreen struct{}

func (*SEOScreen) ScreenTitle() string { return "SEO" }
func (*SEOScreen) ScreenDescription() string {
	return "Per-page SEO: canonical URL, hreflang alternates, JSON-LD article schema, plus sitewide sitemap.xml and robots.txt."
}
func (*SEOScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// ScreenCanonical → <link rel="canonical">.
func (*SEOScreen) ScreenCanonical() string { return "https://gofastr.dev/seo" }

// ScreenHreflangs → one <link rel="alternate" hreflang> per locale.
func (*SEOScreen) ScreenHreflangs() []uihost.HreflangLink {
	return []uihost.HreflangLink{
		{Lang: "en", URL: "https://gofastr.dev/seo"},
		{Lang: "es", URL: "https://gofastr.dev/es/seo"},
		{Lang: "x-default", URL: "https://gofastr.dev/seo"},
	}
}

// ScreenSchema → one <script type="application/ld+json"> per item.
func (*SEOScreen) ScreenSchema() []seo.Thing {
	article := seo.NewArticle()
	article.Headline = "GoFastr SEO module"
	article.Description = "How per-page SEO tags compose in GoFastr."
	article.URL = "https://gofastr.dev/seo"
	article.DatePublished = "2026-06-01"

	bc := seo.NewBreadcrumbList(
		seo.BreadcrumbItem{Name: "Home", URL: "https://gofastr.dev/"},
		seo.BreadcrumbItem{Name: "SEO", URL: "https://gofastr.dev/seo"},
	)
	return []seo.Thing{article, bc}
}

func (s *SEOScreen) Render() render.HTML {
	li := func(children ...render.HTML) render.HTML {
		return html.ListItem(html.ListItemConfig{}, children...)
	}
	return html.Section(html.SectionConfig{Class: "doc-page", Label: "SEO"},
		container(
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("SEO")),
			html.Paragraph(html.TextConfig{Class: "lede"}, render.Text(
				"Per-page SEO is wired through four small interfaces — implement the ones you need, skip the rest. The uihost auto-emits the right tags in <head>. View-source on this page to see them.")),

			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Per-page interfaces")),
			html.UnorderedList(html.ListConfig{},
				li(codeText("app.ScreenDescriber"), render.Text(" → "), codeText(`<meta name="description">`), render.Text(". The most-forgotten SEO tag.")),
				li(codeText("uihost.ScreenCanonical"), render.Text(" → "), codeText(`<link rel="canonical">`), render.Text(". Stops query-string variants fragmenting ranking.")),
				li(codeText("uihost.ScreenHreflangs"), render.Text(" → one "), codeText(`<link rel="alternate">`), render.Text(" per locale.")),
				li(codeText("uihost.ScreenSchema"), render.Text(" → one "), codeText(`<script type="application/ld+json">`), render.Text(" per item. This page emits an Article + a BreadcrumbList.")),
			),

			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Bundle alternative — ScreenSEO")),
			html.Paragraph(html.TextConfig{},
				render.Text("Prefer one method over four? "), codeText("ScreenSEO()"),
				render.Text(" bundles description, canonical, hreflangs, robots, OG, Twitter Card, and JSON-LD into a single declaration. "),
				html.Link(html.LinkConfig{Href: "/seo-bundle", Text: "→ See the ScreenSEO bundle demo"}),
			),

			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sitewide endpoints")),
			html.UnorderedList(html.ListConfig{},
				li(html.Link(html.LinkConfig{Href: "/sitemap.xml", Text: "/sitemap.xml"}), render.Text(" — uihost.WithSitemap. Lists every reachable route.")),
				li(html.Link(html.LinkConfig{Href: "/robots.txt", Text: "/robots.txt"}), render.Text(" — uihost.WithRobots. References the sitemap when both are configured.")),
			),
		),
	)
}

// SEOBundleScreen implements the bundle-style ScreenSEO alternative.
type SEOBundleScreen struct{}

func (*SEOBundleScreen) ScreenTitle() string        { return "SEO bundle (ScreenSEO)" }
func (*SEOBundleScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// ScreenSEO returns every per-page SEO field in one struct. Empty fields fall
// through to the per-concern interface chain.
func (*SEOBundleScreen) ScreenSEO() uihost.SEO {
	article := seo.NewArticle()
	article.Headline = "GoFastr SEO bundle"
	article.Description = "All per-page SEO declared in one struct."

	return uihost.SEO{
		Description: "Bundle-style SEO declaration in one method.",
		Canonical:   "https://gofastr.dev/seo-bundle",
		Hreflangs: []uihost.HreflangLink{
			{Lang: "en", URL: "https://gofastr.dev/seo-bundle"},
			{Lang: "es", URL: "https://gofastr.dev/es/seo-bundle"},
		},
		Robots: "index,follow",
		OG: &uihost.OG{
			Title:       "GoFastr SEO bundle",
			Description: "Per-page SEO from one method.",
			URL:         "https://gofastr.dev/seo-bundle",
			Type:        "article",
		},
		Twitter: &uihost.TwitterCard{
			Card:        "summary_large_image",
			Title:       "GoFastr SEO bundle",
			Description: "Per-page SEO from one method.",
		},
		Schema: []seo.Thing{article},
	}
}

func (s *SEOBundleScreen) Render() render.HTML {
	li := func(t string) render.HTML { return html.ListItem(html.ListItemConfig{}, render.Text(t)) }
	return html.Section(html.SectionConfig{Class: "doc-page", Label: "SEO bundle"},
		container(
			html.Link(html.LinkConfig{Href: "/seo", Text: "← SEO"}),
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("SEO bundle — ScreenSEO")),
			html.Paragraph(html.TextConfig{Class: "lede"}, render.Text(
				"Same tags as the per-concern interfaces — packed into one method. View-source to see every tag the bundle emitted in <head>.")),

			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Bundle vs per-concern")),
			html.UnorderedList(html.ListConfig{},
				li("Empty bundle fields fall through to per-concern interfaces — so a screen can mix both."),
				li("Bundle fields ALWAYS win when non-empty. Don't implement both for the same field expecting per-concern to take precedence."),
				li("Returning zero-value SEO opts the screen out of all per-page emission."),
			),
		),
	)
}
