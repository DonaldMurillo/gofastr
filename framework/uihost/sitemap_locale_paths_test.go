package uihost

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// slugsByLocale lists each locale's own slugs, the way a localized
// catalogue names its pages.
type slugsByLocale struct{}

func (*slugsByLocale) SetParams(map[string]string) {}
func (*slugsByLocale) Render() render.HTML         { return "<p>page</p>" }
func (*slugsByLocale) StaticPaths(ctx context.Context) []map[string]string {
	if _, tag, _ := i18n.PathLocale(ctx); tag == "es" {
		return []map[string]string{{"slug": "acerca"}}
	}
	return []map[string]string{{"slug": "about"}}
}

// The sitemap expanded StaticPaths once with no locale and listed the
// default locale's slugs under /es/: /es/p/about, which the Spanish
// tree does not have, and never /es/p/acerca.
func TestSitemapStaticPathsPerLocale(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/p/:slug", &slugsByLocale{}, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	doc, _ := ds.SitemapXML("")
	for _, want := range []string{"<loc>https://x.test/p/about</loc>", "<loc>https://x.test/es/p/acerca</loc>"} {
		if !strings.Contains(doc, want) {
			t.Errorf("sitemap lacks %s:\n%s", want, doc)
		}
	}
	for _, gone := range []string{"https://x.test/es/p/about", "https://x.test/p/acerca"} {
		if strings.Contains(doc, gone) {
			t.Errorf("sitemap names %s, which that locale does not list:\n%s", gone, doc)
		}
	}
}
