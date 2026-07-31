package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The route table, the component catalog, and the action-JS emptiness check were
// rebuilt on EVERY page render. The route table and the default-theme catalog
// are static after wire time, so they are computed once and cached; the
// action-JS emptiness check no longer concatenates the whole bundle just to
// test for any entries (HasActions). Behavior is preserved (same bytes served).

// TestRouteScriptMarshaledOnce: 5 builds produce identical bytes AND marshal the
// route table exactly once.
func TestRouteScriptMarshaledOnce(t *testing.T) {
	ds := newTestUIHostWithMultipleRoutes()
	want := ds.buildRouteScript()
	for range 4 {
		if got := ds.buildRouteScript(); got != want {
			t.Errorf("cached route script diverged from the first build — behavior must be preserved")
		}
	}
	if got := ds.routeMarshalCount.Load(); got != 1 {
		t.Fatalf("route table marshaled %d times across 5 builds, want 1 (per-request marshal not cached)", got)
	}
}

// TestCatalogScriptMarshaledOnce: the default-theme catalog is marshaled once.
func TestCatalogScriptMarshaledOnce(t *testing.T) {
	// Ensure the process-global registry has at least one entry so the catalog
	// is non-empty (the marshal is what we're counting).
	registry.RegisterStyle("uihost-hotpath-pin", func(style.Theme) string {
		return ".uihost-hotpath-pin{color:red}"
	})
	ds := newTestUIHostWithMultipleRoutes()
	want := catalogJSONScript(ds)
	if want == "" {
		t.Fatal("precondition: catalog should be non-empty for a host with registry entries")
	}
	for range 4 {
		if got := catalogJSONScript(ds); got != want {
			t.Errorf("cached catalog diverged — behavior must be preserved")
		}
	}
	if got := ds.catalogMarshalCount.Load(); got != 1 {
		t.Fatalf("catalog marshaled %d times across 5 builds, want 1 (per-request marshal not cached)", got)
	}
}

// TestHasActionsShortCircuitsEmptiness: a host with no compiled actions reports
// HasActions()==false, and injectChrome omits the actions.js tag without ever
// building the concatenated bundle.
func TestHasActionsShortCircuitsEmptiness(t *testing.T) {
	ds := newTestUIHost() // no CompileActions calls
	if ds.HasActions() {
		t.Fatal("HasActions should be false when no actions are compiled")
	}
	page := ds.injectChrome(`<html><head></head><body>x</body></html>`, "/", "", "")
	if strings.Contains(page, "/__gofastr/actions.js") {
		t.Error("actions.js tag emitted despite HasActions()==false — the emptiness check must short-circuit, not build the bundle")
	}
}
