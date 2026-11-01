package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A CRUD mount's literal subroutes (_batch, _events, llm.md) belong to the
// mount's table. Reading the pattern alone, POST /api/v1/_batch on a table
// named v1 looked like a versioned mount of a table named _batch, so a
// _batch:write token passed and a v1:write token got 403.
func TestCRUDSubroutesScopeToTheirTable(t *testing.T) {
	mount := func(r *router.Router, table, path string) {
		ent := entity.Define(table, entity.EntityConfig{
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
		})
		crud.RegisterCrudRoutes(r, crud.NewCrudHandler(ent, nil), path)
	}
	r := router.New()
	// admitted stands in for the handler: the scope gate passed.
	admitted := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}
	r.Use(RequireAPIScopes("/api"), admitted)
	mount(r, "v1", "/api/v1")    // unversioned table named v1
	mount(r, "v3", "/api/v2/v3") // versioned mount of a table named v3
	mount(r, "posts", "/api/v2/posts")

	cases := []struct {
		method, path string
		scopes       []string
		want         int
	}{
		{"POST", "/api/v1/_batch", []string{"v1:write"}, 204},
		{"POST", "/api/v1/_batch", []string{"_batch:write"}, 403},
		{"DELETE", "/api/v1/_batch", []string{"v1:write"}, 204},
		{"GET", "/api/v1/_events", []string{"v1:read"}, 204},
		{"GET", "/api/v1/_events", []string{"_events:read"}, 403},
		{"GET", "/api/v1/llm.md", []string{"v1:read"}, 204},
		{"GET", "/api/v1/llm.md", []string{"llm.md:read"}, 403},
		{"POST", "/api/v2/v3/_batch", []string{"v3:write"}, 204},
		{"POST", "/api/v2/v3/_batch", []string{"_batch:write"}, 403},
		{"GET", "/api/v2/v3/_events", []string{"v3:read"}, 204},
		{"POST", "/api/v2/posts/_batch", []string{"posts:write"}, 204},
		{"GET", "/api/v2/posts/7", []string{"posts:read"}, 204},
		{"GET", "/api/v1/7", []string{"v1:read"}, 204},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, tc.scopes))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s with %v = %d, want %d", tc.method, tc.path, tc.scopes, rec.Code, tc.want)
		}
	}
}

// The scope is the entity's table, not the mount path's last segment: an
// orders handler mounted at /api/orders/v1 stamped v1, so orders:read was
// refused and v1:read read orders.
func TestCRUDMountScopesToEntityTable(t *testing.T) {
	ent := entity.Define("orders", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	})
	r := router.New()
	r.Use(RequireAPIScopes("/api"), func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	crud.RegisterCrudRoutes(r, crud.NewCrudHandler(ent, nil), "/api/orders/v1")
	for _, tc := range []struct {
		path  string
		scope string
		want  int
	}{
		{"/api/orders/v1", "orders:read", 204},
		{"/api/orders/v1", "v1:read", 403},
		{"/api/orders/v1/7", "orders:read", 204},
		{"/api/orders/v1/_events", "v1:read", 403},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, []string{tc.scope}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("GET %s with %s = %d, want %d", tc.path, tc.scope, rec.Code, tc.want)
		}
	}
}
