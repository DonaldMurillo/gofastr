package openapi

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A scoped token's view of the spec keeps only the entities its scopes
// read: a customers:read token got every schema while /api refused the
// reads (F9).
func TestRequestViewFollowsTokenScopes(t *testing.T) {
	customers := entity.Define("customers", entity.EntityConfig{Table: "customers", Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "name", Type: schema.String}}})
	invoices := entity.Define("invoices", entity.EntityConfig{Table: "invoices", Exposure: &entity.ExposureConfig{Public: true}, Fields: []schema.Field{{Name: "total", Type: schema.Int}}})
	s := EntityOpenAPI(reg(customers, invoices), "Test", "1.0.0", nil)
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	scoped := req.WithContext(access.WithHeldScopes(req.Context(), []string{"customers:read"}))
	paths := getMap(t, s.RequestView(scoped).Build(), "paths")
	if _, ok := paths["/customers"]; !ok {
		t.Error("a customers:read token's view dropped customers")
	}
	if _, ok := paths["/invoices"]; ok {
		t.Error("a customers:read token's view kept invoices")
	}
	paths = getMap(t, s.RequestView(req).Build(), "paths")
	if _, ok := paths["/invoices"]; !ok {
		t.Error("an unscoped caller's view dropped invoices")
	}
}
