package uihost

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestGetScreenSurvivesPostOnlyRoute is the regression test for the
// core bug: registering a POST-only route (e.g. an entity action) at a
// path that also has a uihost screen made GET requests to that path
// return a bare 405 instead of rendering the screen. With the
// MethodNotAllowed hook, GET /shipments now falls through to
// serveOrRender and the screen renders with 200.
func TestGetScreenSurvivesPostOnlyRoute(t *testing.T) {
	a := uiapp.NewApp("survival-test")
	layout := uiapp.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	a.SetDefaultLayout(layout)
	a.RegisterScreen(uiapp.NewScreen("/shipments", &testHomeComp{}).WithTitle("Shipments"), nil)

	ds := New(a)

	// Simulate the host app registering a POST-only entity route at the
	// same path as the screen — the exact scenario that triggered the bug.
	r := router.New()
	r.Post("/shipments", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	ds.Mount(r) // Mount last — claims NotFound + MethodNotAllowed catch-alls.

	// GET /shipments must render the screen (200), not a bare 405.
	req := httptest.NewRequest(http.MethodGet, "/shipments", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /shipments: expected 200 (screen render), got %d body=%q",
			w.Code, truncate(w.Body.String(), 300))
	}
	if !strings.Contains(w.Body.String(), "Home Page") {
		t.Errorf("expected screen content in body, got:\n%s", truncate(w.Body.String(), 500))
	}
}

// TestPost405RendersStyledPage verifies that a POST to a GET-only path
// renders a styled 405 page (not a bare text body) with the Allow
// header preserved. The 405 page composes from the design system — no
// bespoke CSS.
func TestPost405RendersStyledPage(t *testing.T) {
	a := uiapp.NewApp("405-test")
	layout := uiapp.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	a.SetDefaultLayout(layout)
	a.RegisterScreen(uiapp.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)

	ds := New(a)

	r := router.New()
	// A GET-only route — POST to it should 405.
	r.Get("/api/things", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ds.Mount(r)

	req := httptest.NewRequest(http.MethodPost, "/api/things", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%q", w.Code, w.Body.String())
	}
	if allow := w.Header().Get("Allow"); allow != "GET" {
		t.Fatalf("expected Allow: GET, got %q", allow)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<html") {
		t.Errorf("expected styled HTML 405 page, got bare body:\n%s", body)
	}
	if !strings.Contains(body, "405") {
		t.Errorf("expected body to mention 405, got:\n%s", truncate(body, 300))
	}
}

// TestResolvePredicateMatchesServeOrRender pins the agreement between
// resolvesStaticOrScreen (the MethodNotAllowed fall-through predicate)
// and serveOrRender's actual behavior: whenever the predicate says a
// path resolves, serveOrRender must not 404 it — and when it says it
// doesn't, serveOrRender must 404. The two mirror each other's
// resolution steps; this test catches drift.
func TestResolvePredicateMatchesServeOrRender(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "app.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := uiapp.NewApp("pin-test")
	layout := uiapp.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	a.SetDefaultLayout(layout)
	a.RegisterScreen(uiapp.NewScreen("/screen", &testHomeComp{}).WithTitle("S"), nil)

	ds := New(a)
	ds.staticDir = staticDir
	r := router.New()
	ds.Mount(r)

	cases := []struct {
		path    string
		resolve bool
	}{
		{"/screen", true},      // registered screen
		{"/app.css", true},     // static file on disk
		{"/favicon.ico", true}, // favicon shortcut
		{"/nope", false},       // nothing resolves
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if got := ds.resolvesStaticOrScreen(req); got != tc.resolve {
			t.Errorf("predicate(%s) = %v, want %v", tc.path, got, tc.resolve)
			continue
		}
		w := httptest.NewRecorder()
		ds.serveOrRender(w, req)
		is404 := w.Code == http.StatusNotFound
		if tc.resolve && is404 {
			t.Errorf("predicate says %s resolves but serveOrRender 404s", tc.path)
		}
		if !tc.resolve && !is404 {
			t.Errorf("predicate says %s misses but serveOrRender gave %d", tc.path, w.Code)
		}
	}
}
