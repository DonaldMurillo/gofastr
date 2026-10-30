package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A path can match more than one registered pattern: /foo matches both
// "POST /foo" and "GET /{id}". The 405 Allow set is every method the
// path answers under, so the probe cannot stop at the single most
// specific pattern.
func TestAllowListsEveryMatchingPattern(t *testing.T) {
	r := New()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Get("/{id}", noop)
	r.Post("/foo", noop)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/foo", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /foo = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Fatalf("Allow = %q, want %q", got, "GET, HEAD, POST")
	}

	// A path only the wildcard matches still lists only its methods.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/bar", nil))
	if got := w.Header().Get("Allow"); w.Code != http.StatusMethodNotAllowed || got != "GET, HEAD" {
		t.Fatalf("DELETE /bar = %d Allow %q, want 405 Allow %q", w.Code, got, "GET, HEAD")
	}
}

// With "POST /foo" gated off, /foo still answers GET through "GET /{id}":
// a DELETE is a 405 advertising GET, not a 404.
func TestAllowSurvivesGatedSpecificPattern(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool { return key != "POST /foo" })
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Get("/{id}", noop)
	r.Post("/foo", noop)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/foo", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /foo = %d, want 405 (GET /{id} is live)", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want %q (gated POST must not be listed)", got, "GET, HEAD")
	}
}

// A gated GET /foo answers 404 itself (the mux dispatches GET /foo there,
// never to GET /{id}), so it is not an allowed method for /foo.
func TestAllowSkipsMethodShadowedByGatedRoute(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool { return key != "GET /foo" })
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Get("/{id}", noop)
	r.Get("/foo", noop)
	r.Post("/foo", noop)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/foo", nil))
	if got := w.Header().Get("Allow"); w.Code != http.StatusMethodNotAllowed || got != "POST" {
		t.Fatalf("DELETE /foo = %d Allow %q, want 405 Allow %q", w.Code, got, "POST")
	}
}
