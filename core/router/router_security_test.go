package router

import (
	"fmt"
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
	for range 50 {
		go func() {
			r.Use(func(next http.Handler) http.Handler { return next })
			req := httptest.NewRequest(http.MethodGet, "/ping", nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			done <- true
		}()
	}
	for range 50 {
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

// TestRouter_ParamsCatchAll verifies Params() returns the value of a
// catch-all {name...} wildcard under its plain key. Property: Params(r)
// must expose every declared path parameter, including catch-all, so
// callers driving auth / file-path logic off the map don't fail open on
// a silently-missing value.
func TestRouter_ParamsCatchAll(t *testing.T) {
	r := New()
	var got map[string]string
	r.Get("/files/{path...}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = Params(req)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/files/a/b/c", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if got["path"] != "a/b/c" {
		t.Fatalf("SECURITY: [router] Params dropped catch-all value, got %#v. Attack: auth/path logic fails open on empty value.", got)
	}
	if _, leaked := got["path..."]; leaked {
		t.Fatalf("[router] Params exposed catch-all under literal key %q", "path...")
	}
}

// TestRouter_CustomNotFoundKeeps405 verifies that a wrong-method request
// to an existing path still yields native 405 + Allow header even when a
// custom NotFound handler is set. Property: a custom 404 must not mask
// the mux's native Method-Not-Allowed semantics (RFC 7231).
func TestRouter_CustomNotFoundKeeps405(t *testing.T) {
	r := New()
	r.Get("/u/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("nf"))
	}))

	// Wrong method on an existing path -> 405 with Allow, not custom 404.
	req := httptest.NewRequest(http.MethodPost, "/u/1", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("SECURITY: [router] custom NotFound masked native 405, got code=%d body=%q. Attack: lost 405/Allow semantics.", rr.Code, rr.Body.String())
	}
	if allow := rr.Header().Get("Allow"); !strings.Contains(allow, "GET") {
		t.Fatalf("[router] 405 response missing Allow header with GET, got %q", allow)
	}

	// A genuinely unknown path still reaches the custom NotFound.
	req = httptest.NewRequest(http.MethodGet, "/nope", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "nf") {
		t.Fatalf("[router] genuine 404 did not reach custom NotFound, got code=%d body=%q", rr.Code, rr.Body.String())
	}
}

// --- internal-redirect responses ------------------------------------------
//
// Property: a response the mux synthesises itself (trailing-slash
// completion, "//" collapse, "/./" and "/../" cleaning) is still a
// response from this router, so it must be subject to the SAME route
// gate and the SAME middleware chain as a matched route. Before this
// was pinned, net/http's RedirectHandler was served directly out of
// ServeHTTP, which skipped the gate (turning the documented "plain
// 404, existence must not leak" contract into a 307-vs-404 oracle)
// and skipped every middleware — security headers, recovery, request
// logging, CORS and rate limiting. Same class as CVE-2026-15704
// (Eclipse BaSyx) and CVE-2026-33808 (@fastify/express).

// redirectingPaths are the four ways net/http's mux synthesises a
// redirect to the same canonical target.
func redirectingPaths() []string {
	return []string{"/admin/panel", "//admin/panel/", "/admin/./panel/", "/x/../admin/panel/"}
}

func TestRedirectRunsMiddlewareChain(t *testing.T) {
	r := New()
	var ran int
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ran++
			w.Header().Set("X-Sec", "on")
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/admin/panel/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	for _, p := range redirectingPaths() {
		ran = 0
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if ran == 0 || rr.Header().Get("X-Sec") != "on" {
			t.Errorf("SECURITY: [router] %s bypassed the middleware chain (code=%d ran=%d). "+
				"Attack: unauthenticated request served with no security headers, "+
				"logging, recovery or rate limiting.", p, rr.Code, ran)
		}
	}
}

func TestRedirectToGatedRouteIs404(t *testing.T) {
	r := New()
	r.SetRouteGate(func(string) bool { return false })
	r.Get("/admin/panel/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	r.Post("/admin/wipe/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	// Baseline: the canonical path on a fully gated router is a plain 404.
	base := httptest.NewRecorder()
	r.ServeHTTP(base, httptest.NewRequest(http.MethodGet, "/admin/panel/", nil))
	if base.Code != http.StatusNotFound {
		t.Fatalf("[router] gated canonical path returned %d, want 404", base.Code)
	}

	for _, p := range redirectingPaths() {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("SECURITY: [router] gated %s answered %d (Location=%q) instead of 404. "+
				"Attack: unauthenticated redirect-vs-404 oracle enumerates every "+
				"gated subtree, contradicting SetRouteGate's documented contract.",
				p, rr.Code, rr.Header().Get("Location"))
		}
	}

	// 307 preserves method AND body, so the POST shape is the sharp one.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/admin/wipe", strings.NewReader("x=1")))
	if rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [router] gated POST /admin/wipe answered %d (Location=%q), want 404. "+
			"Attack: method+body-preserving 307 confirms a disabled module's route exists.",
			rr.Code, rr.Header().Get("Location"))
	}
}

func TestRedirectStillRedirectsWhenLive(t *testing.T) {
	// Keep the useful half of the behaviour: an ungated trailing-slash
	// completion must still redirect, not 404.
	r := New()
	r.Get("/admin/panel/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	for _, p := range redirectingPaths() {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if rr.Code < 300 || rr.Code >= 400 {
			t.Errorf("[router] ungated %s returned %d, want a redirect", p, rr.Code)
		}
		if loc := rr.Header().Get("Location"); loc != "/admin/panel/" {
			t.Errorf("[router] %s redirected to %q, want /admin/panel/", p, loc)
		}
	}
}

// TestUnmatchedPathWorkIsBounded pins that the 404/405 fallback does not
// do work proportional to the route table on every unmatched request.
// It used to clone the request and rebuild an entire http.ServeMux —
// parsing and tree-inserting every registered pattern — which measured
// ~1200x a matched request (205us / 3704 allocs against 300 routes) on
// a fully attacker-controlled URL, ahead of the rate limiter.
func TestUnmatchedPathWorkIsBounded(t *testing.T) {
	r := New()
	const routes = 300
	for i := range routes {
		r.Get(fmt.Sprintf("/route%d/{id}", i), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	}
	miss := httptest.NewRequest(http.MethodGet, "/nope", nil)
	got := testing.AllocsPerRun(50, func() {
		r.ServeHTTP(httptest.NewRecorder(), miss)
	})
	// A constant-work fallback allocates on the order of tens; a rebuild
	// allocates on the order of the route count.
	if got > routes {
		t.Errorf("SECURITY: [router] unmatched request allocated %.0f objects against %d routes. "+
			"Attack: unauthenticated request amplifies into route-table-proportional "+
			"work ahead of the rate limiter.", got, routes)
	}
}

// --- path-parameter canonicalisation --------------------------------------
//
// Property: a path parameter never carries a byte sequence that Go's own
// mux would have refused to route. The mux cleans dot-segments and
// collapses slashes in the REQUEST path (redirecting when it has to), so
// a "/" inside a single-segment {name}, or a ".." segment in either
// form, can only have arrived percent-encoded — i.e. deliberately
// smuggled past segment matching. Surfaces: single-segment {name},
// catch-all {name...}, via Param and via Params.

func TestParamRejectsEncodedSlash(t *testing.T) {
	r := New()
	var one, rest string
	r.Get("/f/{name}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		one = Param(req, "name")
	}))
	r.Get("/g/{rest...}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rest = Param(req, "rest")
	}))

	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/f/%2e%2e%2f%2e%2e%2fetc%2fpasswd", nil))
	if strings.Contains(one, "/") {
		t.Errorf("SECURITY: [router] single-segment Param returned %q containing a path separator. "+
			"Attack: %%2F-smuggled traversal reaches file/proxy/key sinks that trust Param.", one)
	}

	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/g/a/%2e%2e/%2e%2e/etc/passwd", nil))
	if strings.Contains(rest, "..") {
		t.Errorf("SECURITY: [router] catch-all Param returned %q containing a dot segment. "+
			"Attack: %%2E-smuggled traversal escapes the catch-all's intended subtree.", rest)
	}

	// The legitimate catch-all contract survives: multi-segment values
	// still span segments (see TestRouter_ParamsCatchAll).
	rest = ""
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/g/a/b/c", nil))
	if rest != "a/b/c" {
		t.Errorf("[router] catch-all lost its multi-segment value, got %q want a/b/c", rest)
	}
}

// TestParamsKeepsDeclaredKeys pins that Params exposes every parameter
// the matched pattern declares, even when the value scrubs down to
// empty. Callers gate on `if _, ok := p["id"]; !ok { ...collection... }`,
// so a silently-absent key sends a scrubbed single-item request down the
// collection branch instead of rejecting it.
func TestParamsKeepsDeclaredKeys(t *testing.T) {
	r := New()
	var got map[string]string
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = Params(req)
	}))
	for _, u := range []string{"/users/%0Aadmin", "/users/%00", "/users/%2fadmin"} {
		got = nil
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, u, nil))
		if _, ok := got["id"]; !ok {
			t.Errorf("SECURITY: [router] Params(%s) omitted the declared {id} key entirely (got %#v). "+
				"Attack: callers keyed on presence fail open into the collection branch.", u, got)
		}
	}
}
