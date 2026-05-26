package uihost

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// When the host configures WithFavicon(path) but ships no static file
// at that path, every fresh page load 404s on the favicon. The host
// silently 204s the configured favicon URL so the browser stops
// hammering and the dev console stays quiet.
func TestWithFaviconServesNoContentWhenFileMissing(t *testing.T) {
	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application, WithFavicon("/static/favicon.ico"))

	req := httptest.NewRequest("GET", "/static/favicon.ico", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("expected 204 No Content, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// The fallback only kicks in for the configured favicon URL. Other
// missing paths must still 404 — otherwise the fallback would mask
// real routing bugs.
func TestWithFaviconDoesNotMaskOtherMissingPaths(t *testing.T) {
	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application, WithFavicon("/static/favicon.ico"))

	req := httptest.NewRequest("GET", "/static/something-else.png", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404 for unrelated path, got %d", w.Code)
	}
}
