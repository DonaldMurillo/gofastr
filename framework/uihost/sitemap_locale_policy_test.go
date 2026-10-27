package uihost

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A policy may read the reader's locale: /report is published in English
// and French and not yet in Spanish. The sitemap judged the page once,
// with no locale, and listed /es/report and named it as an alternate.
func TestSitemapPolicyPerLocale(t *testing.T) {
	a := app.NewApp("x")
	a.RegisterScreen(app.NewScreen("/report", &textComp{tag: "report"}).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			if _, tag, _ := i18n.PathLocale(ctx); tag == "es" {
				return app.Decision{Kind: app.DecisionBlock, Status: http.StatusNotFound}
			}
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es", "fr"}})
	doc, _ := ds.SitemapXML("")
	if strings.Contains(doc, "https://x.test/es/report") {
		t.Errorf("sitemap names the blocked /es/report:\n%s", doc)
	}
	for _, want := range []string{
		"<loc>https://x.test/report</loc>", "<loc>https://x.test/fr/report</loc>",
		`hreflang="fr" href="https://x.test/fr/report"`, `hreflang="x-default" href="https://x.test/report"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("sitemap lacks %s:\n%s", want, doc)
		}
	}
}

// Only the default locale blocked: x-default is that page, so it goes too.
func TestSitemapXDefaultFollowsDefault(t *testing.T) {
	a := app.NewApp("x")
	a.RegisterScreen(app.NewScreen("/report", &textComp{tag: "report"}).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			if _, tag, _ := i18n.PathLocale(ctx); tag != "es" {
				return app.Decision{Kind: app.DecisionBlock, Status: http.StatusNotFound}
			}
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	doc, _ := ds.SitemapXML("")
	if !strings.Contains(doc, "<loc>https://x.test/es/report</loc>") {
		t.Errorf("sitemap lacks /es/report:\n%s", doc)
	}
	if strings.Contains(doc, `href="https://x.test/report"`) || strings.Contains(doc, "<loc>https://x.test/report<") {
		t.Errorf("sitemap names the blocked default page:\n%s", doc)
	}
}
