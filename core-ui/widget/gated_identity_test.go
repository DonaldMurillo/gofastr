package widget_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// The host predicate took the widget from a /core-ui/widget/<name>/ URL,
// which an RPC or a custom StatePath does not have (2026-10-27 U32). Each
// gate now hands the predicate the definition it guards.
func TestGatePassesItsWidgetToThePredicate(t *testing.T) {
	var asked []string
	widget.SetSessionCheck(func(r *http.Request) bool {
		d, ok := widget.GatedWidget(r)
		if !ok {
			asked = append(asked, "")
			return false
		}
		asked = append(asked, d.Name)
		return true
	})
	t.Cleanup(func() { widget.SetSessionCheck(nil) })

	ran := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	def := widget.Definition{
		Name:           "gated-identity",
		RequireSession: true,
		StatePath:      "/api/gated-identity/state",
		Signals:        map[string]widget.SignalSource{"n": widget.SignalFunc(func() (any, error) { return 1, nil })},
		RPCs:           []widget.RPCEndpoint{{Method: http.MethodPost, Path: "/api/gated-identity/save", Handler: ran}},
	}
	root := router.New()
	widget.Mount(root.Group("/v2"), &def)

	for _, p := range []struct{ method, path string }{
		{http.MethodGet, def.StateURL()},
		{http.MethodGet, def.ChromeURL()},
		{http.MethodPost, "/v2/api/gated-identity/save"},
	} {
		asked = nil
		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, httptest.NewRequest(p.method, p.path, nil))
		if rec.Code != http.StatusOK || len(asked) != 1 || asked[0] != "gated-identity" {
			t.Errorf("%s %s = %d, predicate saw %q; want 200 and the widget's name", p.method, p.path, rec.Code, asked)
		}
	}

	asked = nil
	if !widget.GateSatisfied(&def, httptest.NewRequest(http.MethodGet, "/some/page", nil)) || len(asked) != 1 || asked[0] != "gated-identity" {
		t.Errorf("GateSatisfied: predicate saw %q, want the widget's name", asked)
	}
	if _, ok := widget.GatedWidget(httptest.NewRequest(http.MethodGet, def.StateURL(), nil)); ok {
		t.Error("a request no gate handed over carries a widget")
	}
}
