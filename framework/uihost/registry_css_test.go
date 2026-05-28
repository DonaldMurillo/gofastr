package uihost

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
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

// TestCSSLoadOrder_AppCSSWinsOverComponentCSS pins the cascade
// ordering for hosts that override framework component defaults.
// app.css is the last <link> in <head>, so a host's customCSS for
// `.ui-button { padding: ... }` wins against the framework default
// at matching specificity without needing `!important` or
// selector-stacking gymnastics.
//
// Regression guard for the inversion introduced in the migration
// from "app.css first / comp-bundle after" to "comp-bundle first /
// app.css last." If a future refactor flips this back, every
// consumer's theme override silently stops working.
func TestCSSLoadOrder_AppCSSWinsOverComponentCSS(t *testing.T) {
	st := registerTestStyle(t, "order")
	ds := newTestUIHostFor(st)
	body := pageBody(t, ds, "/")

	appCSSIdx := strings.Index(body, `href="/__gofastr/app.css"`)
	compIdx := strings.Index(body, "/__gofastr/comp/")
	if compIdx == -1 {
		// Single-component takes the per-component link path; multi
		// takes comp-bundle. Either form must be present.
		compIdx = strings.Index(body, "/__gofastr/comp-bundle.css")
	}
	if appCSSIdx == -1 {
		t.Fatal("app.css link not emitted")
	}
	if compIdx == -1 {
		t.Fatal("component CSS link not emitted")
	}
	if appCSSIdx < compIdx {
		t.Errorf("app.css must load AFTER component CSS so host overrides "+
			"win cascade ties — got app.css at %d, component CSS at %d:\n%s",
			appCSSIdx, compIdx, truncate(body, 800))
	}
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

// TestComponentCSS_BundleDedupesNames guards against a duplicated
// ?names= entry shipping the same component's CSS twice. Bytes shipped
// twice = wasted bandwidth + a real risk that some property cascade
// gets reset by the duplicate.
func TestComponentCSS_BundleDedupesNames(t *testing.T) {
	a := registerTestStyle(t, "dup-a")
	ds := newTestUIHostFor(a)
	// "a,a" should produce exactly one body block, not two.
	url := "/__gofastr/comp-bundle.css?names=" + a.Name() + "," + a.Name()
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	count := strings.Count(body, `[data-fui-comp="`+a.Name()+`"]`)
	if count != 1 {
		t.Errorf("bundle should ship %s exactly once, got %d occurrences", a.Name(), count)
	}
}

func TestComponentCSS_CatalogShipsInlineJSON(t *testing.T) {
	st := registerTestStyle(t, "cat")
	ds := newTestUIHostFor(st)
	body := pageBody(t, ds, "/")
	if !strings.Contains(body, `<script type="application/json" id="gofastr-catalog">`) {
		t.Error("page must embed an inline JSON catalog block")
	}
	if !strings.Contains(body, `"`+st.Name()+`"`) {
		t.Errorf("inline catalog must include %q: %s", st.Name(), truncate(body, 800))
	}
	if !strings.Contains(body, `"loadMode":"auto"`) {
		t.Errorf("default loadMode should be auto: %s", truncate(body, 800))
	}
	// Old endpoint is removed entirely — should 404, not be registered.
	req := httptest.NewRequest("GET", "/__gofastr/catalog.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("catalog.js endpoint should be 404 (removed entirely), got %d", w.Code)
	}
}

func TestComponentCSS_CacheHeaderImmutableOnlyWhenVMatches(t *testing.T) {
	st := registerTestStyle(t, "cache")
	ds := newTestUIHostFor(st)
	theme := ds.ActiveTheme()
	v := st.Entry().VersionFor(theme)

	// Correct v → immutable.
	{
		req := httptest.NewRequest("GET", "/__gofastr/comp/"+st.Name()+".css?v="+v, nil)
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Errorf("matching v should serve immutable, got %q", got)
		}
	}
	// Mismatched v → no-cache (avoids pinning stale URL to fresh body).
	{
		req := httptest.NewRequest("GET", "/__gofastr/comp/"+st.Name()+".css?v=deadbeef", nil)
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if got := w.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
			t.Errorf("mismatched v MUST NOT be immutable, got %q", got)
		}
	}
	// Missing v → no-cache.
	{
		req := httptest.NewRequest("GET", "/__gofastr/comp/"+st.Name()+".css", nil)
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if got := w.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
			t.Errorf("no v MUST NOT be immutable, got %q", got)
		}
	}
}

func TestComponentCSS_RejectsInvalidNames(t *testing.T) {
	ds := newTestUIHost()
	for _, p := range []string{
		"/__gofastr/comp/../etc/passwd.css",
		"/__gofastr/comp/a%2Fb.css",
		"/__gofastr/comp/.css",
	} {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != 404 {
			t.Errorf("expected 404 for %q, got %d", p, w.Code)
		}
	}
}

func TestComponentCSS_BundleEmitsBundleAttr(t *testing.T) {
	a := registerTestStyle(t, "battr-a")
	b := registerTestStyle(t, "battr-b")
	ds := newTestUIHostForMany(a, b)
	body := pageBody(t, ds, "/")
	if !strings.Contains(body, `data-fui-bundle="`) {
		t.Errorf("bundle <link> must carry data-fui-bundle attr for runtime dedup seeding:\n%s", truncate(body, 800))
	}
	// Both names must appear in the bundle attribute. Other test-
	// registered styles (LoadAlways from earlier subtests in the
	// process-global registry) may also appear; we only assert
	// inclusion, not exact equality.
	for _, name := range []string{a.Name(), b.Name()} {
		needle := name + `,`
		needleEnd := `,` + name + `"`
		alone := `"` + name + `"`
		if !strings.Contains(body, `"`+name) && !strings.Contains(body, needle) && !strings.Contains(body, needleEnd) && !strings.Contains(body, alone) {
			t.Errorf("bundle attr missing %q: %s", name, truncate(body, 800))
		}
	}
}

func TestComponentCSS_PageEmbedsInlineCatalogJSON(t *testing.T) {
	st := registerTestStyle(t, "pglink")
	ds := newTestUIHostFor(st)
	body := pageBody(t, ds, "/")
	want := `<script type="application/json" id="gofastr-catalog">`
	if !strings.Contains(body, want) {
		t.Errorf("page must embed inline catalog JSON block %q:\n%s", want, truncate(body, 800))
	}
	// The page must NOT reference the legacy external catalog.js
	// (CSP-blocked + extra round-trip).
	if strings.Contains(body, `src="/__gofastr/catalog.js"`) {
		t.Error("page must NOT reference the legacy /__gofastr/catalog.js external file")
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
