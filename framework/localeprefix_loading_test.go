package framework

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// loadingHostApp serves /pricing with a spinner as its loading content
// and Spanish under /es/, the spinner's label translated.
func loadingHostApp(t *testing.T, opts ...uihost.Option) *App {
	t.Helper()
	core := app.NewApp("prefix-loading")
	core.RegisterScreen(app.NewScreen("/pricing", &prefixPricing{}).WithTitle("Pricing").
		WithLoading(&app.Loading{Show: app.NewContextComponent(func(ctx context.Context) render.HTML {
			return ui.Spinner(ui.SpinnerConfig{Ctx: ctx})
		})}), nil)
	cat := i18n.NewMapCatalog()
	cat.Set("es", hostPricingWord.Key(), i18n.Message{Text: "Precios"})
	cat.Set("es", i18nui.Loading.Key(), i18n.Message{Text: "Cargando"})
	return NewUIHostApp(uihost.New(core, opts...), WithI18n(i18n.NewTranslator(cat, "en")), WithLocalePrefixes())
}

var manifestSrc = regexp.MustCompile(`src="(/__gofastr/manifest\.js\?v=[0-9a-f]+)"`)

// The page links the manifest of its own locale: the /es/ page's
// spinner says "Cargando", the plain page's "Loading…".
func TestLoadingManifestPerLocale(t *testing.T) {
	a := loadingHostApp(t)
	for page, want := range map[string][2]string{
		"/pricing":    {"Loading…", "Cargando"},
		"/es/pricing": {"Cargando", "Loading…"},
	} {
		_, body := getPrefixed(t, a, page)
		m := manifestSrc.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s links no manifest", page)
		}
		_, js := getPrefixed(t, a, m[1])
		if !strings.Contains(js, `"loading":`) || !strings.Contains(js, want[0]) || strings.Contains(js, want[1]) {
			t.Errorf("%s: %s should say %q, not %q:\n%s", page, m[1], want[0], want[1], js)
		}
	}
}

// A ?v= naming another locale's body serves that body, cached for
// good: the bare manifest request carries the default locale, so the
// hash is what tells the handler which page asked.
func TestManifestHashPicksLocale(t *testing.T) {
	a := loadingHostApp(t)
	_, body := getPrefixed(t, a, "/es/pricing")
	m := manifestSrc.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no manifest link")
	}
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, httptest.NewRequest("GET", m[1], nil))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q for a matching ?v=", cc)
	}
	_, plain := getPrefixed(t, a, "/pricing")
	if pm := manifestSrc.FindStringSubmatch(plain); pm == nil || pm[1] == m[1] {
		t.Errorf("the plain and /es/ pages share a manifest URL %v", pm)
	}
}

// The export writes the inline route graph into each page, so each
// locale tree carries its own loading content.
func TestExportLoadingPerLocale(t *testing.T) {
	dir := t.TempDir()
	if err := loadingHostApp(t).ExportStatic(context.Background(), dir, ""); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string][2]string{
		"pricing/index.html":    {"Loading…", "Cargando"},
		"es/pricing/index.html": {"Cargando", "Loading…"},
	} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		_, routes, ok := strings.Cut(string(b), `id="gofastr-routes">`)
		routes, _, _ = strings.Cut(routes, "</script>")
		if !ok || !strings.Contains(routes, want[0]) || strings.Contains(routes, want[1]) {
			t.Errorf("%s: inline routes should say %q, not %q:\n%s", file, want[0], want[1], routes)
		}
	}
}

// The service worker precaches every locale's manifest: an offline
// /es/ page asks for its own ?v=.
func TestPWAPrecachesEveryManifest(t *testing.T) {
	a := loadingHostApp(t, uihost.WithPWA(uihost.PWAConfig{Name: "Shop"}))
	_, sw := getPrefixed(t, a, "/service-worker.js")
	for _, page := range []string{"/pricing", "/es/pricing"} {
		_, body := getPrefixed(t, a, page)
		m := manifestSrc.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s links no manifest", page)
		}
		if !strings.Contains(sw, m[1]) {
			t.Errorf("the service worker does not precache %s's %s", page, m[1])
		}
	}
}
