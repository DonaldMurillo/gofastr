package framework

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

var hostPricingWord = i18n.Define("framework_test.prefix.pricing", "Pricing")

type prefixPricing struct{ component.ContextOnly }

func (prefixPricing) RenderCtx(ctx context.Context) render.HTML {
	word := render.Text(hostPricingWord.Text(ctx))
	return "<h1>" + word + `</h1><a href="/about">` + word + "</a>"
}
func (prefixPricing) ScreenCanonical() string { return "https://shop.test/pricing" }

// prefixHostApp is an app whose UI host serves /pricing and /about,
// with Spanish routed under /es/.
func prefixHostApp(t *testing.T) *App {
	t.Helper()
	core := app.NewApp("prefix")
	core.RegisterScreen(app.NewScreen("/pricing", &prefixPricing{}).WithTitle("Pricing"), nil)
	core.RegisterScreen(app.NewScreen("/about", &prefixPricing{}).WithTitle("About"), nil)
	host := uihost.New(core, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://shop.test"}))
	cat := i18n.NewMapCatalog()
	cat.Set("es", hostPricingWord.Key(), i18n.Message{Text: "Precios"})
	return NewUIHostApp(host, WithI18n(i18n.NewTranslator(cat, "en")), WithLocalePrefixes())
}

func getPrefixed(t *testing.T, a *App, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Code, rec.Body.String()
}

func TestPrefixedScreenSpeaksItsLocale(t *testing.T) {
	code, body := getPrefixed(t, prefixHostApp(t), "/es/pricing")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{` lang="es">`, "Precios", `href="/es/about"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestHreflangPerLocale(t *testing.T) {
	_, body := getPrefixed(t, prefixHostApp(t), "/es/pricing")
	for _, want := range []string{
		`rel="alternate" hreflang="en" href="https://shop.test/pricing">`,
		`rel="alternate" hreflang="es" href="https://shop.test/es/pricing">`,
		`rel="alternate" hreflang="x-default" href="https://shop.test/pricing">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestCanonicalCarriesPrefix(t *testing.T) {
	_, body := getPrefixed(t, prefixHostApp(t), "/es/pricing")
	if !strings.Contains(body, `rel="canonical" href="https://shop.test/es/pricing">`) {
		t.Error("the Spanish page names the English page canonical")
	}
	_, body = getPrefixed(t, prefixHostApp(t), "/pricing")
	if !strings.Contains(body, `rel="canonical" href="https://shop.test/pricing">`) {
		t.Error("the default page lost its canonical")
	}
}

func TestSitemapEveryLocale(t *testing.T) {
	_, body := getPrefixed(t, prefixHostApp(t), "/sitemap.xml")
	for _, want := range []string{
		`xmlns:xhtml="http://www.w3.org/1999/xhtml"`,
		`<loc>https://shop.test/pricing</loc>`,
		`<loc>https://shop.test/es/pricing</loc>`,
		`<xhtml:link rel="alternate" hreflang="es" href="https://shop.test/es/about"/>`,
		`<xhtml:link rel="alternate" hreflang="x-default" href="https://shop.test/about"/>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in\n%s", want, body)
		}
	}
}

func TestManifestPublishesLocales(t *testing.T) {
	_, body := getPrefixed(t, prefixHostApp(t), "/__gofastr/manifest.js")
	if !strings.Contains(body, `window.__gofastr_locales={"default":"en","prefixes":["es"],"base":""};`) {
		t.Fatalf("manifest.js has no locale set:\n%s", body)
	}
}

// A policy redirect names the plain login path with a plain next=; the
// page rewriter puts the reader's prefix on the Location, and the login
// handler's own redirect to next= gets it the same way.
func TestLoginRedirectKeepsPrefix(t *testing.T) {
	core := app.NewApp("prefix")
	core.RegisterScreen(app.NewScreen("/account", &prefixPricing{}).WithPolicy(app.PolicyFunc(
		func(context.Context) app.Decision { return decide.Redirect("/login?next=/account") })), nil)
	cat := i18n.NewMapCatalog()
	cat.Set("es", hostPricingWord.Key(), i18n.Message{Text: "Precios"})
	a := NewUIHostApp(uihost.New(core), WithI18n(i18n.NewTranslator(cat, "en")), WithLocalePrefixes())
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, httptest.NewRequest("GET", "/es/account", nil))
	if got := rec.Header().Get("Location"); got != "/es/login?next=/account" {
		t.Errorf("Location = %q (status %d), want /es/login?next=/account", got, rec.Code)
	}
}

// The export renders the default tree and one /<tag>/ tree per
// prefixed locale, each in its own language.
func TestExportStaticLocaleTrees(t *testing.T) {
	dir := t.TempDir()
	if err := prefixHostApp(t).ExportStatic(context.Background(), dir, ""); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{"pricing/index.html": "<h1>Pricing</h1>", "es/pricing/index.html": "<h1>Precios</h1>"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s", file, want)
		}
	}
}

func TestNotFoundUnderPrefix(t *testing.T) {
	code, body := getPrefixed(t, prefixHostApp(t), "/es/missing")
	if code != 404 {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(body, ` lang="es">`) {
		t.Error("the 404 under /es/ is not in Spanish")
	}
}

// TestScreenMethodUnderPrefix: a screen reached through a locale prefix
// is still a GET resource. POST /es/pricing answers the screen-path 405
// (Allow: GET, HEAD, no session) in the prefix's language, OPTIONS 204s,
// and a POST to an unknown prefixed path keeps its 404.
func TestScreenMethodUnderPrefix(t *testing.T) {
	a := prefixHostApp(t)
	serve := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		a.Router().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	rec := serve("POST", "/es/pricing")
	if rec.Code != 405 {
		t.Fatalf("POST /es/pricing: status %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q", got)
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("minted a session: %v", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, ` lang="es">`) || strings.Contains(body, "Precios") {
		t.Errorf("want the Spanish 405 document without the screen, got %.300s", body)
	}
	if rec := serve("OPTIONS", "/es/pricing"); rec.Code != 204 || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("OPTIONS /es/pricing: status %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := serve("POST", "/es/missing"); rec.Code != 404 {
		t.Errorf("POST /es/missing: status %d, want 404", rec.Code)
	}
	if rec := serve("GET", "/es/pricing"); rec.Code != 200 {
		t.Errorf("GET /es/pricing: status %d", rec.Code)
	}
}
