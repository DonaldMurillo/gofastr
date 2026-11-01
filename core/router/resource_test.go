package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A route registered under a ResourceGroup carries the name into its
// middleware chain; a route outside one carries none.
func TestResourceGroupNamesItsRoutes(t *testing.T) {
	r := New()
	seen := map[string]string{}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			name, ok := RouteResource(req.Context())
			if !ok {
				name = "(none)"
			}
			seen[req.URL.Path] = name
			next.ServeHTTP(w, req)
		})
	})
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	api := r.Group("/api")
	v1 := api.ResourceGroup("v1")
	v1.Post("/v1/_batch", ok)
	v1.Group("/v1").Get("/_events", ok) // a Group keeps the name
	v1.ResourceGroup("other").Get("/other", ok)
	api.Get("/plain", ok)

	for path, want := range map[string]string{
		"/api/v1/_batch":  "v1",
		"/api/v1/_events": "v1",
		"/api/other":      "other",
		"/api/plain":      "(none)",
	} {
		method := http.MethodGet
		if path == "/api/v1/_batch" {
			method = http.MethodPost
		}
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
		if seen[path] != want {
			t.Errorf("%s: resource %q, want %q", path, seen[path], want)
		}
	}
}

// A handler under a ResourceGroup that redispatches its request through
// the router (the entity MCP tools do) reached the next route with the
// first route's name still in the context: an unnamed route must carry
// none.
func TestRedispatchClearsRouteResource(t *testing.T) {
	r := New()
	var seen []string
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			name, ok := RouteResource(req.Context())
			if !ok {
				name = "(none)"
			}
			seen = append(seen, req.URL.Path+"="+name)
			next.ServeHTTP(w, req)
		})
	})
	r.ResourceGroup("orders").Get("/orders", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		inner := req.Clone(req.Context())
		inner.URL.Path = "/plain"
		inner.RequestURI = ""
		r.ServeHTTP(w, inner)
	}))
	r.Get("/plain", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders", nil))
	if len(seen) != 2 || seen[0] != "/orders=orders" || seen[1] != "/plain=(none)" {
		t.Fatalf("resources seen %v; want [/orders=orders /plain=(none)]", seen)
	}
}
