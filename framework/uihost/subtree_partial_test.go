package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// chainTestApp builds an app with a default "site" layout, a /docs screen
// group nested under it, and a direct /about screen, the minimal shape
// that exercises every subtree-partial branch.
func chainTestApp() *app.App {
	application := app.NewApp("t")
	application.SetDefaultLayout(app.NewLayout("site").WithHeader(app.NewStaticComponent("SITE_HEADER")))
	application.Register("/about", &testHomeComp{}, nil)
	g := app.NewScreenGroup("/docs", app.NewLayout("docs").WithSidebar(app.NewStaticComponent("DOCS_NAV")))
	g.Screen(app.NewScreen("intro", &testHomeComp{}), nil)
	g.Screen(app.NewScreen("guide", &testHomeComp{}), nil)
	application.Router.ScreenGroup(g)
	return application
}

func partialGet(t *testing.T, ds *UIHost, path, from string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	if from != "" {
		req.Header.Set("X-Gofastr-From", from)
	}
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("partial %s (from %s): status %d", path, from, w.Code)
	}
	return w
}

// A navigation whose origin shares only the default root must re-render
// the diverging docs layer (sidebar included) but NOT the shared site
// chrome, and must name the shared layer as the swap boundary.
func TestSubtreePartialRendersOnlyDelta(t *testing.T) {
	ds := New(chainTestApp())
	w := partialGet(t, ds, "/docs/intro", "/about")

	if got := w.Header().Get("X-Gofastr-Swap"); got != "l:site" {
		t.Fatalf("X-Gofastr-Swap = %q, want l:site", got)
	}
	body := w.Body.String()
	if strings.Contains(body, "SITE_HEADER") {
		t.Errorf("shared root chrome must not be re-rendered: %s", body)
	}
	if !strings.Contains(body, "DOCS_NAV") {
		t.Errorf("diverging docs layer must be rendered: %s", body)
	}
	if !strings.Contains(body, `data-fui-layout-slot="g:/docs/:docs"`) {
		t.Errorf("rendered layer must carry its slot marker: %s", body)
	}
	if strings.Contains(body, "<main") {
		t.Errorf("partial must never emit <main>: %s", body)
	}
}

// Sibling navigation inside a group shares the whole chain: bare content,
// swap boundary = the innermost shared layer.
func TestSubtreePartialSiblingIsBare(t *testing.T) {
	ds := New(chainTestApp())
	w := partialGet(t, ds, "/docs/guide", "/docs/intro")

	if got := w.Header().Get("X-Gofastr-Swap"); got != "g:/docs/:docs" {
		t.Fatalf("X-Gofastr-Swap = %q, want g:/docs/:docs", got)
	}
	body := w.Body.String()
	if strings.Contains(body, "DOCS_NAV") || strings.Contains(body, "data-fui-layout-key") {
		t.Errorf("fully-shared chain must yield bare content: %s", body)
	}
}

// Without X-Gofastr-From the response is exactly the legacy bare partial,
// old runtimes keep working across a deploy skew.
func TestSubtreePartialWithoutFromIsLegacy(t *testing.T) {
	ds := New(chainTestApp())
	w := partialGet(t, ds, "/docs/intro", "")

	if got := w.Header().Get("X-Gofastr-Swap"); got != "" {
		t.Fatalf("X-Gofastr-Swap must be absent without From, got %q", got)
	}
	if body := w.Body.String(); strings.Contains(body, "DOCS_NAV") {
		t.Errorf("legacy partial must be bare content: %s", body)
	}
}

// A forged/unknown From can only degrade to the bare partial: content,
// policy, and status are unchanged.
func TestSubtreePartialForgedFromDegrades(t *testing.T) {
	ds := New(chainTestApp())
	for _, from := range []string{"/nope", "://bad", "/about/../../etc"} {
		w := partialGet(t, ds, "/docs/intro", from)
		if got := w.Header().Get("X-Gofastr-Swap"); got != "" {
			t.Errorf("from=%q: X-Gofastr-Swap = %q, want absent", from, got)
		}
	}
}

// Runtime-module hints must be classic-script preloads: the loader
// injects classic <script src>, and a rel=modulepreload response is not
// reusable for a classic request (double fetch).
func TestModuleHintIsClassicPreload(t *testing.T) {
	links := runtimeModulePreloadLinks(`<div data-fui-widget="w1"></div>`)
	if links == "" {
		t.Fatal("expected a preload hint for the widgets module")
	}
	if strings.Contains(links, "modulepreload") {
		t.Fatalf("hint must not be modulepreload: %s", links)
	}
	if !strings.Contains(links, `rel="preload" as="script"`) {
		t.Fatalf("hint must be rel=preload as=script: %s", links)
	}
}
