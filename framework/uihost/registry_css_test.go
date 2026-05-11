package uihost

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/registry"
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core/render"
)

// uniqueName picks a name unique to the test run so the
// process-global registry can host multiple tests.
var nameSeq atomic.Int64

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, nameSeq.Add(1))
}

type registeredTestComp struct {
	style *registry.Style
	body  string
}

func (c *registeredTestComp) Render() render.HTML {
	return c.style.Render(&innerSpan{body: c.body})
}

type innerSpan struct{ body string }

func (i *innerSpan) Render() render.HTML {
	return render.HTML(`<section class="x">` + i.body + `</section>`)
}

// registerTestStyle creates a Style handle that returns a minimal CSS
// rule when built. Each call uses a unique name so tests are
// independent.
func registerTestStyle(t *testing.T, prefix string, opts ...registry.Option) *registry.Style {
	t.Helper()
	name := uniqueName(prefix)
	return registry.RegisterStyle(name, func(theme style.Theme) string {
		return style.NewComponentSheet(name, theme).
			Rule(".x").Set("color", "{colors.primary}").End().
			MustBuild()
	}, opts...)
}

func TestComponentCSS_SingleComponentEmitsDirectLink(t *testing.T) {
	st := registerTestStyle(t, "single")
	ds := newTestUIHostFor(st)
	body := pageBody(t, ds, "/")
	want := `<link rel="stylesheet" href="/__gofastr/comp/` + st.Name() + `.css?v=`
	if !strings.Contains(body, want) {
		t.Errorf("missing direct <link>:\n%s", truncate(body, 800))
	}
	if strings.Contains(body, "comp-bundle.css") {
		t.Error("single-component page should not use bundle endpoint")
	}
}

func TestComponentCSS_MultipleComponentsBundleLink(t *testing.T) {
	a := registerTestStyle(t, "bundle-a")
	b := registerTestStyle(t, "bundle-b")
	ds := newTestUIHostForMany(a, b)
	body := pageBody(t, ds, "/")
	if !strings.Contains(body, "/__gofastr/comp-bundle.css?names=") {
		t.Errorf("multi-component page must use bundle endpoint:\n%s", truncate(body, 800))
	}
	// names should appear sorted
	wantNames := []string{a.Name(), b.Name()}
	if wantNames[0] > wantNames[1] {
		wantNames[0], wantNames[1] = wantNames[1], wantNames[0]
	}
	wantNamesStr := strings.Join(wantNames, ",")
	if !strings.Contains(body, "names="+wantNamesStr) {
		t.Errorf("bundle URL must list sorted names %q: %s", wantNamesStr, truncate(body, 800))
	}
}

func TestComponentCSS_EagerLinkEvenWithoutRender(t *testing.T) {
	// Register an entry the page does NOT render, but mark LoadAlways
	// — the SSR host must still emit its <link>.
	st := registerTestStyle(t, "always", registry.WithLoad(registry.LoadAlways))
	// Use the default test host whose home page does NOT render this style.
	ds := newTestUIHost()
	body := pageBody(t, ds, "/")
	want := "/__gofastr/comp/" + st.Name() + ".css"
	if !strings.Contains(body, want) {
		t.Errorf("LoadAlways link missing from page that doesn't render it:\n%s", truncate(body, 800))
	}
}

func TestComponentCSS_ServeIndividualSheetIsScoped(t *testing.T) {
	st := registerTestStyle(t, "serve")
	ds := newTestUIHostFor(st)
	req := httptest.NewRequest("GET", "/__gofastr/comp/"+st.Name()+".css", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	wantSel := `[data-fui-comp="` + st.Name() + `"] .x`
	if !strings.Contains(body, wantSel) {
		t.Errorf("CSS not scoped:\n%s", body)
	}
}

func TestComponentCSS_BundleConcatenates(t *testing.T) {
	a := registerTestStyle(t, "bun-a")
	b := registerTestStyle(t, "bun-b")
	ds := newTestUIHostForMany(a, b)
	url := "/__gofastr/comp-bundle.css?names=" + a.Name() + "," + b.Name()
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `[data-fui-comp="`+a.Name()+`"]`) {
		t.Error("bundle missing component A")
	}
	if !strings.Contains(body, `[data-fui-comp="`+b.Name()+`"]`) {
		t.Error("bundle missing component B")
	}
}

func TestComponentCSS_CatalogJSContainsRegisteredEntries(t *testing.T) {
	st := registerTestStyle(t, "cat")
	ds := newTestUIHostFor(st)
	req := httptest.NewRequest("GET", "/__gofastr/catalog.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "window.__gofastr_catalog =") {
		t.Error("catalog.js must define window.__gofastr_catalog")
	}
	if !strings.Contains(body, `"`+st.Name()+`"`) {
		t.Errorf("catalog must include %q: %s", st.Name(), body)
	}
	if !strings.Contains(body, `"loadMode":"auto"`) {
		t.Errorf("default loadMode should be auto: %s", body)
	}
}

func TestComponentCSS_PageLinksCatalogScript(t *testing.T) {
	st := registerTestStyle(t, "pglink")
	ds := newTestUIHostFor(st)
	body := pageBody(t, ds, "/")
	if !strings.Contains(body, `<script src="/__gofastr/catalog.js"></script>`) {
		t.Error("page must include catalog.js script")
	}
}

// --- helpers ---

// homeComponent renders the registered style on the home page.
type homeWithStyles struct{ styles []*registry.Style }

func (h *homeWithStyles) Render() render.HTML {
	var out strings.Builder
	for _, s := range h.styles {
		out.WriteString(string(s.Render(&innerSpan{body: "x"})))
	}
	return render.HTML(out.String())
}

// newTestUIHostFor builds a host whose home page renders the given
// styled component.
func newTestUIHostFor(s *registry.Style) *UIHost {
	return newTestUIHostForMany(s)
}

func newTestUIHostForMany(ss ...*registry.Style) *UIHost {
	application := app.NewApp("Test App")
	layout := app.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	application.SetDefaultLayout(layout)
	application.RegisterScreen(
		app.NewScreen("/", &homeWithStyles{styles: ss}).WithTitle("Home"),
		nil,
	)
	return New(application)
}

func pageBody(t *testing.T, ds *UIHost, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	return w.Body.String()
}
