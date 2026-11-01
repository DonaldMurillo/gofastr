package uihost

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type strayMarkerScreen struct{ styles []*registry.Style }

func (s *strayMarkerScreen) Render() render.HTML {
	return render.HTML(string((&homeWithStyles{styles: s.styles}).Render()) +
		`<div data-cui-comp="not-registered-anywhere">stray</div>`)
}

// A marker that names no registered style (host markup, a typo, a CMS
// field, a component this replica lacks mid-deploy) must not reach the
// bundle URL or its data-cui-bundle list. It did: the bundle answered 404,
// and the runtime, seeded from the list, never fetched the registered
// sheets itself, so every component on the page stayed unstyled. The same
// list drives the inline first-paint block's data-cui-bundle-href.
func TestUnknownMarkerDoesNotBreakTheBundle(t *testing.T) {
	registry.IsolateForTest(t)
	a := registerTestStyle(t, "stray-a")
	b := registerTestStyle(t, "stray-b")
	application := app.NewApp("Stray")
	application.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, &testFooterComp{}))
	application.RegisterScreen(app.NewScreen("/", &strayMarkerScreen{styles: []*registry.Style{a, b}}).WithTitle("Home"), nil)
	ds := New(application)
	body := linkedPageBody(t, ds, "/")

	if strings.Contains(body, "not-registered-anywhere,") || strings.Contains(body, ",not-registered-anywhere") ||
		strings.Contains(body, "names=not-registered-anywhere") {
		t.Fatalf("an unregistered marker reached the bundle link: %s", truncate(body, 1200))
	}
	m := regexp.MustCompile(`href="(/__gofastr/comp-bundle\.css\?[^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no bundle link: %s", truncate(body, 1200))
	}
	href := strings.ReplaceAll(m[1], "&amp;", "&")
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", href, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), a.Name()) || !strings.Contains(w.Body.String(), b.Name()) {
		t.Fatalf("GET %s = %d, want 200 carrying both sheets; body %q", href, w.Code, truncate(w.Body.String(), 300))
	}
}

// A bundle URL another replica built can name a component this one does
// not have. The bundle serves the sheets it knows rather than refusing
// them all, and a bundle that skipped a name is never cached as
// immutable, even when its v is the hash of the names it did serve.
func TestBundleSkipsUnknownNames(t *testing.T) {
	registry.IsolateForTest(t)
	a := registerTestStyle(t, "skip-a")
	ds := newTestUIHostFor(a)
	theme := ds.activeTheme()
	e, _ := registry.Lookup(a.Name())
	known := hashStrings(e.VersionForHash(theme, style.ThemeHash(theme)))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/__gofastr/comp-bundle.css?names="+a.Name()+"&v="+known, nil))
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("control: the known-only bundle at its own hash is %q, want immutable (the hash spelling drifted)", cc)
	}
	w = httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/__gofastr/comp-bundle.css?names="+a.Name()+",gone-in-this-build&v="+known, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), a.Name()) {
		t.Fatalf("status %d body %q, want 200 with the known sheet", w.Code, truncate(w.Body.String(), 300))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	w = httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/__gofastr/comp-bundle.css?names=gone-a,gone-b", nil))
	if w.Code != 404 {
		t.Errorf("a bundle of only unknown names = %d, want 404", w.Code)
	}
}
