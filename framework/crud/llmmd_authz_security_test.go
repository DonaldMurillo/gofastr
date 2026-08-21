package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// /{table}/llm.md checked for a session but not for the entity's declared
// permission, while List checks both. An authenticated user with no
// `orders:read` grant got 403 on the data and 200 on the schema, every field
// name, type and enum of an entity they cannot read.
//
// This is the REST twin of the MCP tools/list disclosure: the schema is the
// disclosure, not the row.
func TestLLMMDRequiresTheSamePermissionAsList(t *testing.T) {
	ch := ordersHandlerWithReadPermission(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/orders/llm.md", nil)
	req = reqWithRolesOnly(req, "u1") // signed in, no orders:read grant
	LLMMDHandlerFor(ch).ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("SECURITY: [disclosure] llm.md served the schema of an entity the caller cannot read (status %d)", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "internal_note") {
		t.Error("SECURITY: [disclosure] refused response still carried field names")
	}
}

// An anonymous caller was already refused; keep it that way.
func TestLLMMDStillRefusesAnonymous(t *testing.T) {
	ch := ordersHandlerWithReadPermission(t)
	rec := httptest.NewRecorder()
	LLMMDHandlerFor(ch).ServeHTTP(rec, httptest.NewRequest("GET", "/orders/llm.md", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("SECURITY: [disclosure] llm.md served an anonymous caller")
	}
}

// A caller who holds the permission must still get the docs, the whole point
// of the endpoint is that an agent with access can read it.
func TestLLMMDServesPermittedCaller(t *testing.T) {
	ch := ordersHandlerWithReadPermission(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/orders/llm.md", nil)
	LLMMDHandlerFor(ch).ServeHTTP(rec, reqWithGrant(req, "u1", "orders:read"))

	if rec.Code != http.StatusOK {
		t.Fatalf("llm.md refused a permitted caller: %d %s", rec.Code, rec.Body.String())
	}
}

func ordersHandlerWithReadPermission(t *testing.T) *CrudHandler {
	t.Helper()
	ent := entity.Define("orders", entity.EntityConfig{Fields: []schema.Field{
		{Name: "total", Type: schema.Int},
		{Name: "internal_note", Type: schema.String},
	}, Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "orders:read"}},
	})
	return &CrudHandler{Entity: ent}
}

// TestLLMMDIndexHidesUngrantedEntities is the registry-index twin of the
// per-entity llm.md disclosure above. RegistryLLMMDHandler served a
// construction-time-precomputed document listing EVERY registered entity
// (name, base path, endpoint count, soft-delete/multi-tenant flags) to any
// authenticated caller, while each entity's List runs the full scope chain.
// An authenticated caller with no orders:read grant got 403 on the rows and
// 200 on the index, the index itself is the disclosure. The fix renders the
// index per request, filtered to the entities this caller can actually list
// (the same read-scope predicate List runs).
func TestLLMMDIndexHidesUngrantedEntities(t *testing.T) {
	orders := entity.Define("orders", entity.EntityConfig{Fields: []schema.Field{
		{Name: "total", Type: schema.Int},
	}, Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "orders:read"}}})
	products := entity.Define("products", entity.EntityConfig{Fields: []schema.Field{
		{Name: "name", Type: schema.String},
	}, Exposure: &entity.ExposureConfig{}}) // no read permission → any caller can list
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"orders":   orders,
		"products": products,
	}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/llm.md", nil)
	req = reqWithRolesOnly(req, "u1") // signed in, holds no orders:read grant
	RegistryLLMMDHandler(reg, "Test API").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("index refused an authenticated caller: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "products") {
		t.Error("SECURITY: index hid an entity the caller CAN read (products)")
	}
	if strings.Contains(body, "orders") {
		t.Error("SECURITY: [disclosure] index listed orders — name/base path/flags of an entity the caller cannot read")
	}
}

// TestLLMMDIndexShowsGrantedEntity confirms a caller who holds the grant still
// sees the entity, so the filter is a scope predicate, not a blanket suppress.
func TestLLMMDIndexShowsGrantedEntity(t *testing.T) {
	orders := entity.Define("orders", entity.EntityConfig{Fields: []schema.Field{
		{Name: "total", Type: schema.Int},
	}, Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "orders:read"}}})
	reg := stubRegistry{byName: map[string]*entity.Entity{"orders": orders}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/llm.md", nil)
	req = reqWithGrant(req, "u1", "orders:read")
	RegistryLLMMDHandler(reg, "Test API").ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "orders") {
		t.Error("index hid orders from a caller who holds orders:read")
	}
}
