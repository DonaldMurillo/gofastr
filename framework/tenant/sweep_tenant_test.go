package tenant

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestApplyTenantFilterWithNoTenantMatchesNothing(t *testing.T) {
	qb := query.Select("id").From("items")
	ApplyTenantFilter(qb, "")
	sqlStr, args := qb.Build()
	if !strings.Contains(sqlStr, "1=0") || len(args) != 0 {
		t.Fatalf("empty tenant built %q %v, want a predicate that can never be true", sqlStr, args)
	}
}

func TestWithMultiTenantKeepsADeclaredColumn(t *testing.T) {
	ent := &entity.Entity{Config: entity.EntityConfig{Name: "notes", Fields: []schema.Field{{Name: "id", Type: schema.UUID}, {Name: "org_id", Type: schema.String}}}}
	WithMultiTenant(ent, TenantConfig{Field: "org_id"})
	if n := len(ent.Config.Fields); n != 2 {
		t.Fatalf("fields = %d, want the declared org_id reused, not added again", n)
	}
}

func TestResolveMiddlewareRefusesNilResolve(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ResolveMiddleware(nil) did not panic")
		}
	}()
	ResolveMiddleware(nil)
}
