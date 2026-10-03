package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestRouteManifestOmitsLayoutFieldsWhenEmpty pins the Opt-in
// decision's manifest arm: every field the layout work added (layer
// keys, deferred outlets, per-route loading) is omitted when empty
// (docs/DESIGN-layout-outlets.md "### Opt-in": "manifest fields are
// omitted when empty"). A plain site's route table carries no layout
// bytes at all.
func TestRouteManifestOmitsLayoutFieldsWhenEmpty(t *testing.T) {
	a := app.NewApp("M")
	a.RegisterScreen(app.NewScreen("/", &plainSiteComp{marker: "H"}).WithTitle("Home"), nil)
	h := New(a)
	s := h.buildRouteScriptUncached()
	for _, field := range []string{"layouts", "deferred", "loading", "loadingAfter", "loadingMin", "docScripts", "preload"} {
		if strings.Contains(s, `"`+field+`"`) {
			t.Errorf("plain route's manifest carries %q: %s", field, s)
		}
	}
}
