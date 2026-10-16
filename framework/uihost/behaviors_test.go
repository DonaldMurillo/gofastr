package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The behaviour seam's host-side wiring (docs/spec-behavior-registry.md
// "Serving and the manifest"): live pages learn registered behaviours'
// markers from /__gofastr/manifest.js (window.__gofastr_behaviors),
// export mode keeps the inline #gofastr-behaviors block, and the
// preload scan covers a registered behaviour's marker.

// behMarkerScreen renders the isolated test behaviour's marker, so the
// host's preload scan finds it in the page HTML.
type behMarkerScreen struct{}

func (behMarkerScreen) Render() render.HTML {
	return render.HTML(`<div data-x="1">marker</div>`)
}

func registerTestBehavior(t *testing.T) {
	t.Helper()
	registry.IsolateForTest(t)
	registry.RegisterBehavior("beh", `(function(){});`, registry.Markers("[data-x]"),
		registry.Requires("action"))
}

// manifest.js carries the behaviours global with the registered
// behaviour's markers.
func TestManifestJSCarriesBehaviors(t *testing.T) {
	registerTestBehavior(t)
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/__gofastr/manifest.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	want := `window.__gofastr_behaviors={"beh":{"s":["[data-x]"],"r":["action"]}}`
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("manifest.js missing %q; body: %s", want, w.Body.String())
	}
}

// An exported page (RenderStaticPage) is self-contained: it carries the
// inline #gofastr-behaviors block instead of the external manifest.
func TestExportPageCarriesInlineBehaviorsBlock(t *testing.T) {
	registerTestBehavior(t)
	ds := actionsHost()

	page, err := ds.RenderStaticPage(context.Background(), "/plain")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}
	if !strings.Contains(page, `id="gofastr-behaviors"`) {
		t.Error("exported page missing the inline #gofastr-behaviors block")
	}
	if !strings.Contains(page, `"beh"`) {
		t.Error("inline behaviors block does not list the registered behaviour")
	}
}

// A live page does NOT carry the inline block; it reads the behaviours
// global from manifest.js, mirroring TestPageExternalizesDataBlocks for
// the module manifest.
func TestLivePageHasNoInlineBehaviorsBlock(t *testing.T) {
	registerTestBehavior(t)
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/plain", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	page := w.Body.String()
	if strings.Contains(page, `id="gofastr-behaviors"`) {
		t.Error("inline behaviors block emitted on a live page — live pages read window.__gofastr_behaviors from manifest.js")
	}
	if !strings.Contains(page, "/__gofastr/manifest.js?v=") {
		t.Error("live page missing its manifest.js reference")
	}
}

// A page whose HTML carries the marker gets a preload link for the
// registered behaviour's module, content-addressed like every other.
func TestPreloadLinkForBehaviorMarker(t *testing.T) {
	registerTestBehavior(t)

	a := app.NewApp("BehPreload")
	a.Register("/mark", &behMarkerScreen{}, nil)
	ds := New(a)

	req := httptest.NewRequest("GET", "/mark", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	page := w.Body.String()
	want := `href="/__gofastr/runtime/beh.js?v=`
	if !strings.Contains(page, want) {
		t.Errorf("page carrying [data-x] missing preload link %q", want)
	}
}

// A registered data block (registry.RegisterDataBlock, core-ui/localdb's
// schema among them) reaches the head exactly once in both modes: the
// live branch writes it beside the compute manifest, and export mode
// gets it through widget.RuntimeModuleManifestScript.
func TestDataBlockOncePerPageLiveAndExport(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterDataBlock("gofastr-test-block", func() []byte { return []byte(`{"k":1}`) })
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/plain", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	exported, err := ds.RenderStaticPage(context.Background(), "/plain")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}
	for _, c := range []struct{ mode, page string }{{"live", w.Body.String()}, {"export", exported}} {
		if n := strings.Count(c.page, `id="gofastr-test-block"`); n != 1 {
			t.Errorf("%s page carries the data block %d times; want 1", c.mode, n)
		}
	}
}
