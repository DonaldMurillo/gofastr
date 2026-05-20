package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// SEOBundleScreen demonstrates the ScreenSEO bundle-style alternative
// to implementing the per-concern interfaces individually.
type SEOBundleScreen struct{}

func (*SEOBundleScreen) ScreenTitle() string {
	return "SEO bundle (ScreenSEO)"
}
func (*SEOBundleScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// ScreenSEO returns every per-page SEO field in one struct. Any
// empty field falls through to the per-concern interface chain — so
// you can mix-and-match if you have a screen that's mostly bundle
// but pulls one value from a different source.
func (*SEOBundleScreen) ScreenSEO() uihost.SEO {
	article := seo.NewArticle()
	article.Headline = "GoFastr SEO bundle"
	article.Description = "All per-page SEO declared in one struct."

	return uihost.SEO{
		Description: "Bundle-style SEO declaration in one method.",
		Canonical:   "https://example.com/components/seo-bundle",
		Hreflangs: []uihost.HreflangLink{
			{Lang: "en", URL: "https://example.com/components/seo-bundle"},
			{Lang: "es", URL: "https://example.com/es/components/seo-bundle"},
		},
		Robots: "index,follow",
		OG: &uihost.OG{
			Title:       "GoFastr SEO bundle",
			Description: "Per-page SEO from one method.",
			URL:         "https://example.com/components/seo-bundle",
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
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/seo", "class": "doc-back"},
			render.Text("← SEO")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("SEO bundle — ScreenSEO")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Same SEO surface as the per-concern interfaces — packed into one method. View-source to see every tag the bundle emitted in <head>.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("This screen's ScreenSEO()")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`func (*SEOBundleScreen) ScreenSEO() uihost.SEO {
    return uihost.SEO{
        Description: "Bundle-style SEO declaration in one method.",
        Canonical:   "https://example.com/components/seo-bundle",
        Hreflangs:   []uihost.HreflangLink{
            {Lang: "en", URL: "https://example.com/components/seo-bundle"},
            {Lang: "es", URL: "https://example.com/es/components/seo-bundle"},
        },
        Robots:  "index,follow",
        OG:      &uihost.OG{Title: "GoFastr SEO bundle", ...},
        Twitter: &uihost.TwitterCard{Card: "summary_large_image", ...},
        Schema:  []seo.Thing{article},
    }
}`))),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Bundle vs per-concern")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Empty bundle fields fall through to per-concern interfaces — so a screen can mix both.")),
			render.Tag("li", nil, render.Text("Bundle fields ALWAYS win when non-empty. Don't implement both for the same field expecting per-concern to take precedence.")),
			render.Tag("li", nil, render.Text("Returning zero-value SEO from ScreenSEO opts the screen out of all per-page emission (no description, no canonical, no JSON-LD).")),
		),
	)
}
