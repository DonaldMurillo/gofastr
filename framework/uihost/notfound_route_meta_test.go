package uihost

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// barcodeComp is a screen whose Load answers ErrNotFound for an
// unknown type, and whose own SEO names a description and canonical.
type barcodeComp struct{ typ string }

func (c *barcodeComp) SetParams(p map[string]string) { c.typ = p["type"] }
func (c *barcodeComp) Load(context.Context) error {
	if c.typ != "qr" {
		return app.ErrNotFound
	}
	return nil
}
func (c *barcodeComp) Render() render.HTML { return "<p>barcode " + render.HTML(c.typ) + "</p>" }
func (c *barcodeComp) ScreenSEO(context.Context) SEO {
	return SEO{Description: "Barcode page", Canonical: "/barcodes/" + c.typ}
}

type nfMetaBody struct{}

func (nfMetaBody) Render() render.HTML { return "<p>themed-404</p>" }

var routeMetaRe = regexp.MustCompile(`<(?:link|meta)[^>]*data-cui-page-meta[^>]*>`)

// routeMeta returns the page-meta tags of a response, minus the site
// description, which every page carries. A partial carries its head
// escaped in a template attribute, so the body is unescaped first.
func routeMeta(body string) []string {
	var out []string
	for _, tag := range routeMetaRe.FindAllString(html.UnescapeString(body), -1) {
		if strings.Contains(tag, `name="description"`) && strings.Contains(tag, "Site wide") {
			continue
		}
		out = append(out, tag)
	}
	return out
}

// A screen that answers ErrNotFound renders the 404 an unmatched URL
// renders: none of the matched route's hreflang alternates, canonical
// or description, on the full page and on a client navigation (#823).
func TestNotFoundOutcomeCarriesNoRouteMeta(t *testing.T) {
	a := app.NewApp("nfmeta")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	a.Register("/barcodes/:type", &barcodeComp{}, nil)
	key := app.NewKey[string]("nfmeta-row")
	a.RegisterScreen(app.NewScreen("/rows/:type", &barcodeComp{}).
		WithDescription("Row page").
		Resolve(key.From(func(context.Context) (string, error) { return "", app.ErrNotFound })).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			_, _ = key.Get(ctx)
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	a.Register("/origin", statusScreenComp("origin"), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}),
		WithNotFoundScreen(nfMetaBody{}), WithDescription("Site wide"))
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es", "fr"}}
	ds.SetPathLocales(set)

	// The found page carries the route's meta, so the check below is not vacuous.
	found := serveNF(ds, set, "/barcodes/qr", false)
	if found.Code != http.StatusOK || !strings.Contains(found.Body.String(), `hreflang="es"`) {
		t.Fatalf("found page: %d, want hreflang alternates:\n%s", found.Code, found.Body.String())
	}

	for _, path := range []string{"/barcodes/nope", "/rows/qr", "/zz-nope"} {
		for _, partial := range []bool{false, true} {
			rec := serveNF(ds, set, path, partial)
			body := rec.Body.String()
			if rec.Code != http.StatusNotFound || !strings.Contains(body, "themed-404") {
				t.Fatalf("%s partial=%v: %d, want the themed 404:\n%s", path, partial, rec.Code, body)
			}
			if !strings.Contains(body, "Site wide") {
				t.Errorf("%s partial=%v: 404 lost the site description", path, partial)
			}
			if got := routeMeta(body); len(got) != 0 {
				t.Errorf("%s partial=%v: 404 carries route meta %q", path, partial, got)
			}
			if strings.Contains(body, "hreflang") {
				t.Errorf("%s partial=%v: 404 carries hreflang", path, partial)
			}
		}
	}
}

func serveNF(ds *UIHost, set i18n.PathLocales, path string, partial bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(i18n.WithPathLocale(req.Context(), set, "en"))
	if partial {
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-From", "/origin")
	}
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	return rec
}

// seoOutletComp is a route whose own SEO names a description and a
// canonical, rendered as the 404-outlet outcome.
type seoOutletComp struct{}

func (seoOutletComp) Render() render.HTML { return "<p>outlet route</p>" }
func (seoOutletComp) ScreenSEO(context.Context) SEO {
	return SEO{Description: "Outlet route", Canonical: "/missing-seo"}
}

// The 404-outlet outcome is a not-found page too: none of the route's
// meta, though the route's own component declares some.
func TestNotFoundOutletCarriesNoRouteMeta(t *testing.T) {
	a := outlet404App()
	a.RegisterScreen(app.NewScreen("/missing-seo", seoOutletComp{}).WithDescription("Outlet route"), nil)
	a.Register("/origin", statusScreenComp("origin"), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}), WithDescription("Site wide"))
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	ds.SetPathLocales(set)
	if rec := serveNF(ds, set, "/present", false); !strings.Contains(rec.Body.String(), `hreflang="es"`) {
		t.Fatalf("present: %d, want hreflang alternates", rec.Code)
	}
	for _, path := range []string{"/missing", "/missing-seo"} {
		for _, partial := range []bool{false, true} {
			rec := serveNF(ds, set, path, partial)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s partial=%v: status %d, want 404", path, partial, rec.Code)
			}
			if got := routeMeta(rec.Body.String()); len(got) != 0 {
				t.Errorf("%s partial=%v: 404-outlet page carries route meta %q", path, partial, got)
			}
		}
	}
}

// A static export's 404.html renders at "/", which resolves to the home
// route; it carries none of the home page's meta.
func TestStaticNotFoundPageCarriesNoRouteMeta(t *testing.T) {
	a := app.NewApp("nfstatic")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	a.RegisterScreen(app.NewScreen("/", statusScreenComp("home")).WithDescription("Home page"), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}), WithNotFoundScreen(nfMetaBody{}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	page, ok := ds.RenderStaticNotFoundPage(context.Background(), "/")
	if !ok || !strings.Contains(page, "themed-404") {
		t.Fatalf("ok=%v, want the themed 404:\n%s", ok, page)
	}
	if got := routeMeta(page); len(got) != 0 {
		t.Errorf("404.html carries route meta %q", got)
	}
}

// The error page of a matched route carries none of its meta either.
func TestErrorPageCarriesNoRouteMeta(t *testing.T) {
	a := app.NewApp("errmeta")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	key := app.NewKey[string]("errmeta-row")
	a.RegisterScreen(app.NewScreen("/boom/:type", &barcodeComp{}).
		WithDescription("Boom page").
		Resolve(key.From(func(context.Context) (string, error) { return "", errors.New("db down") })).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			_, _ = key.Get(ctx)
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	a.Register("/origin", statusScreenComp("origin"), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://example.com"}), WithDescription("Site wide"))
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	ds.SetPathLocales(set)
	for _, partial := range []bool{false, true} {
		rec := serveNF(ds, set, "/boom/qr", partial)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("partial=%v: status %d, want 500", partial, rec.Code)
		}
		if got := routeMeta(rec.Body.String()); len(got) != 0 {
			t.Errorf("partial=%v: error page carries route meta %q", partial, got)
		}
	}
}
