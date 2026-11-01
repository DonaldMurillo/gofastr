package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// driveRoutedAPIScopes serves one request through a router carrying
// RequireAPIScopes("/api") and the given GET/POST patterns, the way an
// app mounts its CRUD tree, so the middleware sees the matched pattern.
func driveRoutedAPIScopes(t *testing.T, patterns []string, method, path string, scopes []string) int {
	t.Helper()
	r := router.New()
	r.Use(RequireAPIScopes("/api"))
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, p := range patterns {
		r.Get(p, ok)
		r.Post(p, ok)
	}
	req := httptest.NewRequest(method, path, nil)
	if scopes != nil {
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, scopes))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

// A version-like segment is skipped only when the route says a table
// follows it. A table genuinely named "v1"/"v2" must still require its own
// scope, and its record ids must never be read as the resource.
func TestAPIScopes_VersionSegmentNeverSwallowsResource(t *testing.T) {
	versioned := []string{"/api/v1/posts", "/api/v1/posts/{id}", "/api/v2/posts", "/api/v1.2/posts"}
	v1Table := []string{"/api/v1", "/api/v1/{id}", "/api/v2"}
	cases := []struct {
		name         string
		routes       []string
		method, path string
		scopes       []string
		want         int
	}{
		// The feature: /v1/posts and /v2/posts share the resource "posts".
		{"v1 prefix maps to posts", versioned, "GET", "/api/v1/posts", []string{"posts:read"}, 200},
		{"v2 prefix maps to posts", versioned, "GET", "/api/v2/posts", []string{"posts:read"}, 200},
		{"versioned record maps to posts", versioned, "GET", "/api/v1/posts/7", []string{"posts:read"}, 200},
		{"minor version prefix", versioned, "GET", "/api/v1.2/posts", []string{"posts:read"}, 200},
		{"versioned write needs write", versioned, "POST", "/api/v1/posts", []string{"posts:read"}, 403},
		{"versioned path rejects wrong resource", versioned, "GET", "/api/v1/posts", []string{"orders:read"}, 403},

		// The bypass: an entity whose table is literally version-shaped.
		{"table named v1 still gated", v1Table, "GET", "/api/v1", []string{"orders:read"}, 403},
		{"table named v1 allowed with scope", v1Table, "GET", "/api/v1", []string{"v1:read"}, 200},
		{"table named v2 write gated", v1Table, "POST", "/api/v2", []string{"v2:read"}, 403},

		// A record id spelled like another table is still a v1 record.
		{"v1 record id is not a resource", v1Table, "GET", "/api/v1/orders", []string{"orders:read"}, 403},
		{"v1 record needs v1 scope", v1Table, "GET", "/api/v1/orders", []string{"v1:read"}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := driveRoutedAPIScopes(t, tc.routes, tc.method, tc.path, tc.scopes); got != tc.want {
				t.Errorf("%s %s with %v = %d, want %d", tc.method, tc.path, tc.scopes, got, tc.want)
			}
		})
	}
}

// Outside a mux the path alone cannot say whether v1 is a version or a
// table, so every candidate resource is required.
func TestAPIScopes_UnroutedNeedsEveryCandidate(t *testing.T) {
	cases := []struct {
		path   string
		scopes []string
		want   int
	}{
		{"/api/v1/orders", []string{"orders:read"}, 403},
		{"/api/v1/orders", []string{"v1:read"}, 403},
		{"/api/v1/orders", []string{"v1:read", "orders:read"}, 200},
		{"/api/v1", []string{"v1:read"}, 200},
		{"/api/v1/", []string{"orders:read"}, 403},
	}
	for _, c := range cases {
		if got := driveAPIScopes(t, "/api", "GET", c.path, c.scopes); got != c.want {
			t.Errorf("GET %s with %v = %d, want %d", c.path, c.scopes, got, c.want)
		}
	}
}
