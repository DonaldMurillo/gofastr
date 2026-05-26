package uihost

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// Screens registered with a trailing slash should also resolve when the
// client requests the same path without the slash — net/http.ServeMux
// gives this for free for raw subtrees, but here the path dispatch
// happens inside the UIHost via App.Router.Resolve, so we have to do
// it ourselves. A miss should redirect, not 404.
func TestUIHostRedirectsMissingTrailingSlash(t *testing.T) {
	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/components/", &testHomeComp{}).WithTitle("C"), nil)
	ds := New(application)

	req := httptest.NewRequest("GET", "/components", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 301 {
		t.Fatalf("expected 301 redirect, got %d: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/components/" {
		t.Fatalf("expected Location=/components/, got %q", loc)
	}
}

// Query string must survive the redirect.
func TestUIHostTrailingSlashRedirectKeepsQuery(t *testing.T) {
	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/items/", &testHomeComp{}).WithTitle("I"), nil)
	ds := New(application)

	req := httptest.NewRequest("GET", "/items?sort=name", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 301 {
		t.Fatalf("expected 301, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/items/?sort=name" {
		t.Fatalf("Location lost query string: %q", loc)
	}
}

// When nothing resolves either way, it must still 404 (no silent
// redirect to a non-existent path).
func TestUIHostNoRedirectWhenSlashFormAlsoMisses(t *testing.T) {
	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application)

	req := httptest.NewRequest("GET", "/nope", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
