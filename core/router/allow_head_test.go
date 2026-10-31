package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ServeMux answers HEAD on a GET route, so a 405 Allow names HEAD
// beside GET, and a gated GET takes its HEAD with it (2026-10-27 P12).
func TestAllowNamesImplicitHead(t *testing.T) {
	r := New()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Get("/items", noop)
	r.Post("/orders", noop)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/items", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD /items = %d, want 200", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/items", nil))
	if got := w.Header().Get("Allow"); w.Code != http.StatusMethodNotAllowed || got != "GET, HEAD" {
		t.Fatalf("POST /items = %d Allow %q, want 405 Allow %q", w.Code, got, "GET, HEAD")
	}
	// No GET on the path: HEAD is not offered.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/orders", nil))
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Fatalf("DELETE /orders Allow %q, want %q", got, "POST")
	}

	// An explicit HEAD route is listed once.
	both := New()
	both.Get("/a", noop)
	both.Handle(http.MethodHead, "/a", noop)
	w = httptest.NewRecorder()
	both.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/a", nil))
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("explicit HEAD: Allow %q, want %q", got, "GET, HEAD")
	}

	gated := New()
	gated.SetRouteGate(func(key string) bool { return key != "GET /items" })
	gated.Get("/items", noop)
	gated.Put("/items", noop)
	w = httptest.NewRecorder()
	gated.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/items", nil))
	if got := w.Header().Get("Allow"); got != "PUT" {
		t.Fatalf("gated GET: Allow %q, want %q", got, "PUT")
	}
}
