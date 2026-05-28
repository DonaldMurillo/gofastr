package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRouter_MethodNotAllowed verifies that POST to a GET-only route
// returns 405, not 200 or 404. Attack: accessing routes via wrong method.
func TestRouter_MethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/users/123", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Errorf("SECURITY: [router] POST to GET-only route returned 200. Attack: method bypass.")
	}
}

// TestRouter_PathParamNotTampered verifies that path parameters match
// the registered pattern, not arbitrary path segments.
func TestRouter_PathParamNotTampered(t *testing.T) {
	r := New()
	var gotID string
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotID = Param(req, "id")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/users/abc123", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if gotID != "abc123" {
		t.Errorf("path param = %q, want %q", gotID, "abc123")
	}
}

// TestRouter_GroupIsolation verifies that routes registered in one group
// don't leak into another. Attack: route collision between groups.
func TestRouter_GroupIsolation(t *testing.T) {
	r := New()
	admin := r.Group("/admin")
	public := r.Group("/public")

	called := ""
	admin.Get("/secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "admin"
		w.WriteHeader(http.StatusOK)
	}))
	public.Get("/hello", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "public"
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/secret", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if called != "admin" {
		t.Errorf("admin route not matched, got called=%q", called)
	}

	called = ""
	req = httptest.NewRequest(http.MethodGet, "/public/hello", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if called != "public" {
		t.Errorf("public route not matched, got called=%q", called)
	}
}

// TestRouter_CatchAllDoesNotLeak verifies that a catch-all route doesn't
// serve as a fallback for routes that should 404.
func TestRouter_CatchAllDoesNotLeak(t *testing.T) {
	r := New()
	r.Get("/api/{path...}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("api"))
	}))

	// /api/anything should match
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("catch-all /api/* didn't match: status %d", rr.Code)
	}

	// /other should NOT match
	req = httptest.NewRequest(http.MethodGet, "/other", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Errorf("SECURITY: [router] catch-all /api/* matched /other. Attack: route scope leak.")
	}
}

// TestRouter_NotFoundCustom verifies that custom 404 handlers work.
// Attack: default 404 leaking server information.
func TestRouter_NotFoundCustom(t *testing.T) {
	r := New()
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("custom-404"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("custom 404 handler returned status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "custom-404") {
		t.Errorf("custom 404 body = %q, want custom-404", body)
	}
}

// TestRouter_RoutesIntrospection verifies that Routes() doesn't leak
// internal implementation details. Attack: route enumeration.
func TestRouter_RoutesIntrospection(t *testing.T) {
	r := New()
	r.Get("/public", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	r.Get("/admin/secret", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	routes := r.Routes()
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
	// Routes() intentionally returns EVERY route — its callers are the
	// MCP introspection bridge, debug endpoints, and admin tooling.
	// Use [RoutesFiltered] when exposing the list to non-admin clients;
	// the assertion below pins the public-vs-admin separation in that
	// path.
	filtered := r.RoutesFiltered(func(rt RegisteredRoute) bool {
		return rt.Pattern == "/admin/secret"
	})
	if len(filtered) != 1 || filtered[0].Pattern != "/public" {
		t.Errorf("SECURITY: [router] RoutesFiltered did not hide admin path. Got %#v. Attack: route enumeration via introspection endpoint.", filtered)
	}
}

// TestRouter_ConcurrentRegistration verifies that concurrent Use() and
// ServeHTTP() don't race. Attack: race condition crash via concurrent
// route registration.
func TestRouter_ConcurrentRegistration(t *testing.T) {
	r := New()
	r.Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func() {
			r.Use(func(next http.Handler) http.Handler { return next })
			req := httptest.NewRequest(http.MethodGet, "/ping", nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			done <- true
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}

func TestRouter_ParamStripsNewlines(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/x", nil)
	req.SetPathValue("id", "42\nadmin")

	got := Param(req, "id")
	if got != "42" {
		t.Fatalf("SECURITY: [router] Param retained newline/control payload %q. Attack: path-parameter smuggling into downstream headers/queries.", got)
	}
}

func TestRouter_ParamStripsNUL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/x", nil)
	req.SetPathValue("id", "42\x00admin")

	got := Param(req, "id")
	if got != "42" {
		t.Fatalf("SECURITY: [router] Param retained NUL/control payload %q. Attack: path-parameter smuggling into downstream protocol fields.", got)
	}
}

func TestRouter_ParamsStripsNewlines(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/x/posts/y", nil)
	req.Pattern = "GET /users/{id}/posts/{postId}"
	req.SetPathValue("id", "42\nadmin")
	req.SetPathValue("postId", "7")

	got := Params(req)
	if got["id"] != "42" {
		t.Fatalf("SECURITY: [router] Params retained newline/control payload %q. Attack: bulk path-param smuggling into downstream consumers.", got["id"])
	}
}

func TestRouter_ParamsStripsNUL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/x/posts/y", nil)
	req.Pattern = "GET /users/{id}/posts/{postId}"
	req.SetPathValue("id", "42\x00admin")
	req.SetPathValue("postId", "7")

	got := Params(req)
	if got["id"] != "42" {
		t.Fatalf("SECURITY: [router] Params retained NUL/control payload %q. Attack: bulk path-param smuggling into downstream consumers.", got["id"])
	}
}
