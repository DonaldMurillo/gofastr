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
// `orders:read` grant got 403 on the data and 200 on the schema — every field
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

// A caller who holds the permission must still get the docs — the whole point
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
	ent := entity.Define("orders", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "total", Type: schema.Int},
			{Name: "internal_note", Type: schema.String},
		},
		Access: entity.AccessControl{Read: "orders:read"},
	})
	return &CrudHandler{Entity: ent}
}
