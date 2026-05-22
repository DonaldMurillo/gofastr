package routegroup_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

func TestGroupPrefix(t *testing.T) {
	r := router.New()
	g := routegroup.New(r, "/api")
	if g.Prefix() != "/api" {
		t.Errorf("Prefix() = %q, want %q", g.Prefix(), "/api")
	}
}

func TestGroupNormalizesPrefix(t *testing.T) {
	r := router.New()
	tests := []struct{ in, want string }{
		{"api", "/api"},
		{"/api/", "/api"},
		{"", ""},
		{"/", ""},
		{"/api/v1", "/api/v1"},
	}
	for _, tt := range tests {
		g := routegroup.New(r, tt.in)
		if got := g.Prefix(); got != tt.want {
			t.Errorf("New(r, %q).Prefix() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGroupRoutesReachable(t *testing.T) {
	r := router.New()
	g := routegroup.New(r, "/api")

	called := false
	g.Get("/health", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !called {
		t.Error("handler was not called for /api/health")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGroupMiddlewareApplies(t *testing.T) {
	r := router.New()
	var order []string

	g := routegroup.New(r, "/api", routegroup.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "group-mw")
			next.ServeHTTP(w, req)
		})
	}))

	g.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if len(order) != 2 || order[0] != "group-mw" || order[1] != "handler" {
		t.Errorf("middleware order = %v, want [group-mw handler]", order)
	}
}

func TestGroupAccess(t *testing.T) {
	r := router.New()

	g := routegroup.New(r, "/admin", routegroup.WithAccess(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			// Don't call next — simulate denied access
		})
	}))

	g.Get("/dashboard", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (access denied)", rec.Code, http.StatusForbidden)
	}
}

func TestNestedGroup(t *testing.T) {
	r := router.New()

	api := routegroup.New(r, "/api")
	v1 := api.Group("/v1")

	called := false
	v1.Get("/users", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !called {
		t.Error("nested group handler was not called for /api/v1/users")
	}
}

func TestNestedGroupMiddlewareOrder(t *testing.T) {
	r := router.New()
	var order []string

	api := routegroup.New(r, "/api", routegroup.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "api")
			next.ServeHTTP(w, req)
		})
	}))

	v1 := api.Group("/v1", routegroup.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "v1")
			next.ServeHTTP(w, req)
		})
	}))

	v1.Get("/users", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Outer-to-inner: api → v1 → handler
	if len(order) != 3 || order[0] != "api" || order[1] != "v1" || order[2] != "handler" {
		t.Errorf("middleware order = %v, want [api v1 handler]", order)
	}
}

func TestMCPToolName(t *testing.T) {
	r := router.New()

	// No namespace
	g := routegroup.New(r, "/api")
	if got := g.MCPToolName("users", "list"); got != "users.list" {
		t.Errorf("no namespace: got %q, want %q", got, "users.list")
	}

	// With namespace
	g2 := routegroup.New(r, "/admin", routegroup.WithMCPNamespace("admin"))
	if got := g2.MCPToolName("users", "list"); got != "admin.users.list" {
		t.Errorf("with namespace: got %q, want %q", got, "admin.users.list")
	}
}

func TestOpenAPITag(t *testing.T) {
	r := router.New()

	g := routegroup.New(r, "/api")
	if g.OpenAPITag() != "" {
		t.Errorf("default OpenAPITag = %q, want empty", g.OpenAPITag())
	}

	g2 := routegroup.New(r, "/api", routegroup.WithOpenAPITag("API"))
	if g2.OpenAPITag() != "API" {
		t.Errorf("OpenAPITag = %q, want %q", g2.OpenAPITag(), "API")
	}
}

func TestGroupDoesNotAffectOtherRoutes(t *testing.T) {
	r := router.New()

	api := routegroup.New(r, "/api", routegroup.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Api-Group", "true")
			next.ServeHTTP(w, req)
		})
	}))

	api.Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Root-level route — NOT in the group
	r.Get("/health", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Group route should have the middleware header
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Api-Group"); got != "true" {
		t.Errorf("/api/ping X-Api-Group = %q, want %q", got, "true")
	}

	// Root route should NOT have it
	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if got := rec2.Header().Get("X-Api-Group"); got != "" {
		t.Errorf("/health X-Api-Group = %q, want empty", got)
	}
}

func TestMultipleGroupsIndependence(t *testing.T) {
	r := router.New()

	g1 := routegroup.New(r, "/a", routegroup.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Group", "a")
			next.ServeHTTP(w, req)
		})
	}))
	g2 := routegroup.New(r, "/b", routegroup.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Group", "b")
			next.ServeHTTP(w, req)
		})
	}))

	g1.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	g2.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	// /a/test → X-Group: a
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/a/test", nil))
	if got := rec1.Header().Get("X-Group"); got != "a" {
		t.Errorf("/a/test X-Group = %q, want %q", got, "a")
	}

	// /b/test → X-Group: b
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/b/test", nil))
	if got := rec2.Header().Get("X-Group"); got != "b" {
		t.Errorf("/b/test X-Group = %q, want %q", got, "b")
	}
}
