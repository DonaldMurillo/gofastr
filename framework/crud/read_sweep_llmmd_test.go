package crud

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A pipe inside an enum value is escaped too, so the Fields row keeps its
// columns.
func TestLLMMDEscapesPipeInsideEnumValue(t *testing.T) {
	ent := entity.Define("tickets", entity.EntityConfig{
		Name: "tickets", Table: "tickets",
		Fields: []schema.Field{{Name: "status", Type: schema.Enum, Values: []string{"a|b", "c"}}},
	}.WithTimestamps(false))
	line := findLLMMDLine(t, EntityLLMMD(ent), "`status`")
	if n := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|"); n != 6 {
		t.Fatalf("row %q has %d unescaped pipes, want 6", line, n)
	}
}

// A filter example whose bare key reads two ways (the id key beside an
// id_in field) is spelled with brackets, the form the route accepts.
func TestLLMMDExampleBracketsAmbiguousKey(t *testing.T) {
	ent := entity.Define("deals", entity.EntityConfig{
		Name: "deals", Table: "deals",
		Fields: []schema.Field{{Name: "id_in", Type: schema.String}},
	}.WithTimestamps(false))
	doc := EntityLLMMD(ent)
	if !strings.Contains(doc, "`id[in]=") || strings.Contains(doc, "`id_in=a,b,c`") {
		t.Fatalf("the _in example is not bracketed:\n%s", doc)
	}
}

// The llm.md index hides an entity whose List would refuse the caller for
// want of an owner or a tenant on ctx.
func TestLLMMDIndexHidesEntitiesWithoutScopeContext(t *testing.T) {
	owned := entity.Define("notes", entity.EntityConfig{Name: "notes", Table: "notes",
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"}}.WithTimestamps(false))
	tenanted := entity.Define("orgs", entity.EntityConfig{Name: "orgs", Table: "orgs",
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
		Scope:  &entity.ScopeConfig{MultiTenant: true}}.WithTimestamps(false))
	plain := entity.Define("pages", entity.EntityConfig{Name: "pages", Table: "pages",
		Fields: []schema.Field{{Name: "body", Type: schema.String}}}.WithTimestamps(false))
	ctx := context.Background()
	if !canListEntity(ctx, plain) {
		t.Fatal("control: an unscoped entity is hidden")
	}
	if canListEntity(ctx, owned) {
		t.Error("an owner-scoped entity is listed for a ctx with no owner")
	}
	if canListEntity(ctx, tenanted) {
		t.Error("a multi-tenant entity is listed for a ctx with no tenant")
	}
}
