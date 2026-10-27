package uihost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A public page whose policy reads a resolver: the sitemap ran the
// policy with no resolver state, the read failed with ErrNoResolver,
// and the page vanished from the sitemap.
func TestSitemapPolicyReadsResolver(t *testing.T) {
	plan := app.NewKey[string]("plan")
	a := app.NewApp("x")
	a.RegisterScreen(app.NewScreen("/plans", &textComp{tag: "plans"}).
		Resolve(plan.From(func(context.Context) (string, error) { return "free", nil })).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			if v, err := plan.Get(ctx); err != nil || v != "free" {
				return app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
			}
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	a.RegisterScreen(app.NewScreen("/private", &textComp{tag: "private"}).
		WithPolicy(app.PolicyFunc(func(context.Context) app.Decision {
			return app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
		})), nil)
	// A resolver the policy reads fails: the page answers an error.
	broken := app.NewKey[string]("broken")
	a.RegisterScreen(app.NewScreen("/broken", &textComp{tag: "broken"}).
		Resolve(broken.From(func(context.Context) (string, error) { return "", errors.New("down") })).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			_, _ = broken.Get(ctx)
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	doc, _ := ds.SitemapXML("")
	if !strings.Contains(doc, "https://x.test/plans</loc>") {
		t.Errorf("sitemap misses the public /plans:\n%s", doc)
	}
	for _, p := range []string{"/private</loc>", "/broken</loc>"} {
		if strings.Contains(doc, p) {
			t.Errorf("sitemap lists %s:\n%s", p, doc)
		}
	}
}

type catalogComp struct{}

func (*catalogComp) SetParams(map[string]string) {}
func (catalogComp) Render() render.HTML          { return `<a href="/about">about</a>` }
func (catalogComp) ScreenSEO(context.Context) SEO {
	return SEO{Description: "Catalog", Canonical: "/catalog"}
}

// A dynamic route's pattern doc (/p/:id/llm.md) under /es/ kept the
// English canonical, had no hreflangs and linked the English pages,
// unlike the per-URL doc beside it.
func TestPatternLLMMDFollowsLocale(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/p/:id", &catalogComp{}, nil)
	ds := New(a, WithPublicLLMMD(), WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	ds.SetPathLocales(set)
	req := httptest.NewRequest(http.MethodGet, "/p/:id/llm.md", nil)
	req = req.WithContext(i18n.WithPathLocale(req.Context(), set, "es"))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	md := rec.Body.String()
	for _, want := range []string{`canonical: "/es/catalog"`, `- "es"`, "(/es/about)"} {
		if !strings.Contains(md, want) {
			t.Errorf("pattern doc lacks %s:\n%s", want, md)
		}
	}
}

type pricingComp struct{}

func (pricingComp) Render() render.HTML {
	return `<p>See <a href="/about">about</a>, <a href="/__gofastr/x.css">css</a> and <a href="/es/faq">faq</a>.</p>`
}
func (pricingComp) ScreenSEO(context.Context) SEO {
	return SEO{Description: "Plans", Canonical: "/pricing"}
}

// /es/pricing/llm.md is the Spanish page's other representation: it
// kept the English canonical, no generated hreflangs, and body links
// to the English pages, while the HTML localizes all three.
func TestPageLLMMDFollowsLocalePrefix(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/pricing", pricingComp{}, nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation(),
		WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	ds.SetPathLocales(set)
	for _, accept := range []string{"", "text/markdown"} {
		path := "/pricing/llm.md"
		if accept != "" {
			path = "/pricing"
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(i18n.WithPathLocale(req.Context(), set, "es"))
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		md := rec.Body.String()
		for _, want := range []string{
			`canonical: "/es/pricing"`, `- "es"`, `- "x-default"`,
			"(/es/about)", "(/__gofastr/x.css)", "(/es/faq)",
		} {
			if !strings.Contains(md, want) {
				t.Errorf("%s: markdown lacks %s:\n%s", path, want, md)
			}
		}
	}
}
