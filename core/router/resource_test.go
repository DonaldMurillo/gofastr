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
