package routegroup_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// TestVerbsRegisterUnderPrefix drives every verb registrar, Use, Handle,
// Router, and the MCP namespace accessors: each registration must answer
// on the prefixed path with the group middleware applied.
func TestVerbsRegisterUnderPrefix(t *testing.T) {
	r := router.New()
	g := routegroup.New(r, "/api", routegroup.WithMCPNamespace("admin"))
	g.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Group", "yes")
			next.ServeHTTP(w, req)
		})
	})
	echo := func(tag string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tag)) })
	}
	g.Post("/items", echo("post"))
	g.Put("/items/{id}", echo("put"))
	g.Delete("/items/{id}", echo("delete"))
	g.Patch("/items/{id}", echo("patch"))
	g.Handle(http.MethodOptions, "/items", echo("options"))
	if g.Router() == nil {
		t.Fatal("Router() returned nil")
	}
	if got := g.MCPNamespace(); got != "admin" {
		t.Fatalf("MCPNamespace = %q, want admin", got)
	}
	if got := g.MCPToolName("users", "list"); got != "admin.users.list" {
		t.Fatalf("MCPToolName = %q", got)
	}
	for _, c := range []struct{ method, path, want string }{
		{http.MethodPost, "/api/items", "post"},
		{http.MethodPut, "/api/items/1", "put"},
		{http.MethodDelete, "/api/items/1", "delete"},
		{http.MethodPatch, "/api/items/1", "patch"},
		{http.MethodOptions, "/api/items", "options"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != c.want {
			t.Fatalf("%s %s: code=%d body=%q, want 200 %q", c.method, c.path, rec.Code, rec.Body.String(), c.want)
		}
		if rec.Header().Get("X-Group") != "yes" {
			t.Fatalf("%s %s: group middleware did not run", c.method, c.path)
		}
	}
}
