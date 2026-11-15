package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The llm.md route RegisterCrudRoutes mounts answers to the List route's
// read gate, not to the session check alone.
func TestRegisteredLLMMDRouteIsReadGated(t *testing.T) {
	r := router.New()
	RegisterCrudRoutes(r, ordersHandlerWithReadPermission(t), "/orders")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, reqWithRolesOnly(httptest.NewRequest(http.MethodGet, "/orders/llm.md", nil), "u1"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("registered llm.md answered %d to a caller without orders:read, want 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, reqWithGrant(httptest.NewRequest(http.MethodGet, "/orders/llm.md", nil), "u1", "orders:read"))
	if rec.Code != http.StatusOK {
		t.Fatalf("registered llm.md answered %d to a caller with orders:read, want 200", rec.Code)
	}
}

// The mounted llm.md names the path the app mounted the handler at, and
// every route answers to its table whatever that path is.
func TestRegisteredRoutesNameTheirMount(t *testing.T) {
	ent := entity.Define("orders", entity.EntityConfig{Name: "orders", Table: "orders",
		Fields: []schema.Field{{Name: "total", Type: schema.Int}}}.WithTimestamps(false))
	ch := &CrudHandler{Entity: ent, BasePath: "/api/v1"}
	r := router.New()
	var resource string
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			resource, _ = router.RouteResource(req.Context())
			next.ServeHTTP(w, req)
		})
	})
	RegisterCrudRoutes(r, ch, "/shop")
	req := httptest.NewRequest(http.MethodGet, "/shop/llm.md", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Resource: `/api/v1/orders`") {
		t.Fatalf("llm.md = %d, want Resource /api/v1/orders: %.300s", rec.Code, rec.Body.String())
	}
	if resource != "orders" {
		t.Fatalf("route resource = %q, want orders", resource)
	}
}
