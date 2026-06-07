package crud

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// covRelEntity builds a posts entity with one of each relation kind so the
// LLM-md and nested-filter helpers exercise every label branch.
func covRelEntity() *entity.Entity {
	return entity.Define("posts", entity.EntityConfig{
		Name:       "posts",
		Table:      "posts",
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "views", Type: schema.Int, Default: 0},
			{Name: "rating", Type: schema.Float},
			{Name: "price", Type: schema.Decimal},
			{Name: "published", Type: schema.Bool},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}},
			{Name: "uid", Type: schema.UUID},
			{Name: "ts", Type: schema.Timestamp},
			{Name: "d", Type: schema.Date},
			{Name: "meta", Type: schema.JSON},
			{Name: "rel", Type: schema.Relation},
			{Name: "img", Type: schema.Image},
			{Name: "doc", Type: schema.File},
			{Name: "slug", Type: schema.String, Unique: true},
			{Name: "secret", Type: schema.String, Hidden: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
			entity.HasMany("comments", "comments", "post_id"),
			entity.HasOne("profile", "profiles", "post_id"),
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "/posts/{id}/publish", Description: "Publish a post"},
			{Method: "POST", Path: "/posts/{id}/archive"},
		},
	}.WithTimestamps(false))
}

func TestEntityLLMMD_CoversAllBranches(t *testing.T) {
	ent := covRelEntity()
	md := EntityLLMMD(ent)
	for _, want := range []string{
		"# posts", "## Fields", "**required**", "auto", "unique",
		"default:", "values: draft|published", "## Includes",
		"scoped eager-load", "## Endpoints", "Custom Endpoints",
		"Publish a post", "/posts/{id}/archive",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("EntityLLMMD missing %q", want)
		}
	}
	if strings.Contains(md, "secret") {
		t.Error("EntityLLMMD leaked Hidden field")
	}
}

func TestEntityLLMMD_MultiTenantNote(t *testing.T) {
	ent := entity.Define("acct", entity.EntityConfig{
		Name: "acct", Table: "acct", MultiTenant: true,
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	md := EntityLLMMD(ent)
	if !strings.Contains(md, "Multi-tenancy") {
		t.Error("expected multi-tenancy note")
	}
}

func TestRegistryLLMMD_ListsEntities(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"posts": covRelEntity(),
		"acct": entity.Define("acct", entity.EntityConfig{
			Name: "acct", Table: "acct", MultiTenant: true, SoftDelete: true,
			Fields: []schema.Field{{Name: "n", Type: schema.String}},
		}.WithTimestamps(false)),
	}}
	md := RegistryLLMMD(reg, "MyApp")
	for _, want := range []string{"MyApp — API Reference", "posts", "acct", "soft-delete", "multi-tenant", "Quick Reference"} {
		if !strings.Contains(md, want) {
			t.Errorf("RegistryLLMMD missing %q", want)
		}
	}
}

func TestRegistryLLMMD_EmptyAndDefaultTitle(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{}}
	md := RegistryLLMMD(reg, "")
	if !strings.Contains(md, "API — API Reference") {
		t.Errorf("default title missing: %s", md[:40])
	}
	if !strings.Contains(md, "No entities registered.") {
		t.Error("expected empty-registry note")
	}
}

func TestSanitizeDefault_Variants(t *testing.T) {
	if got := sanitizeDefault(strings.Repeat("x", 60)); !strings.HasSuffix(got, "…") {
		t.Errorf("long string not truncated: %q", got)
	}
	if got := sanitizeDefault(42); got != "42" {
		t.Errorf("int = %q", got)
	}
	if got := sanitizeDefault(true); got != "true" {
		t.Errorf("bool = %q", got)
	}
	if got := sanitizeDefault([]int{1}); !strings.Contains(got, "[]int") {
		t.Errorf("complex type = %q", got)
	}
}

func TestFieldTypeLabel_AllTypes(t *testing.T) {
	cases := map[schema.FieldType]string{
		schema.String: "string", schema.Text: "text", schema.Int: "integer",
		schema.Float: "float", schema.Decimal: "decimal", schema.Bool: "boolean",
		schema.Enum: "enum", schema.UUID: "uuid", schema.Timestamp: "timestamp",
		schema.Date: "date", schema.JSON: "json", schema.Relation: "relation",
		schema.Image: "image", schema.File: "file",
	}
	for ft, want := range cases {
		if got := fieldTypeLabel(ft); got != want {
			t.Errorf("fieldTypeLabel(%v) = %q, want %q", ft, got, want)
		}
	}
	if got := fieldTypeLabel(schema.FieldType(9999)); got != "string" {
		t.Errorf("unknown type fallback = %q", got)
	}
}

func TestRelationTypeLabel_AllTypes(t *testing.T) {
	cases := map[entity.RelationType]string{
		entity.RelHasOne: "has-one", entity.RelHasMany: "has-many",
		entity.RelManyToOne: "belongs-to", entity.RelManyToMany: "many-to-many",
	}
	for rt, want := range cases {
		if got := relationTypeLabel(rt); got != want {
			t.Errorf("relationTypeLabel(%v) = %q, want %q", rt, got, want)
		}
	}
	if got := relationTypeLabel(entity.RelationType(99)); got != "unknown" {
		t.Errorf("unknown relation fallback = %q", got)
	}
}

func TestMcpFieldSchema_AllTypes(t *testing.T) {
	cases := []struct {
		f    schema.Field
		want string
	}{
		{schema.Field{Type: schema.Int}, "integer"},
		{schema.Field{Type: schema.Float}, "number"},
		{schema.Field{Type: schema.Decimal}, "number"},
		{schema.Field{Type: schema.Bool}, "boolean"},
		{schema.Field{Type: schema.JSON}, "object"},
		{schema.Field{Type: schema.Enum, Values: []string{"a"}}, "string"},
		{schema.Field{Type: schema.String}, "string"},
	}
	for _, c := range cases {
		got := mcpFieldSchema(c.f)
		if got["type"] != c.want {
			t.Errorf("mcpFieldSchema(%v) type = %v, want %v", c.f.Type, got["type"], c.want)
		}
	}
}

func TestMcpSchemas_Shapes(t *testing.T) {
	ent := covRelEntity()
	if s := idToolSchema(); s["type"] != "object" {
		t.Error("idToolSchema bad shape")
	}
	ls := listToolSchema(ent)
	props := ls["properties"].(map[string]any)
	if _, ok := props["secret"]; ok {
		t.Error("listToolSchema leaked Hidden field")
	}
	if _, ok := props["title"]; !ok {
		t.Error("listToolSchema missing visible field")
	}
	us := updateToolSchema(ent)
	uprops := us["properties"].(map[string]any)
	if _, ok := uprops["id"]; !ok {
		t.Error("updateToolSchema missing id")
	}
	if req, ok := us["required"].([]string); !ok || len(req) != 1 || req[0] != "id" {
		t.Errorf("updateToolSchema required = %v", us["required"])
	}
}

func TestOpToSQL_AllOps(t *testing.T) {
	cases := map[filter.FilterOp]string{
		filter.OpEq: "=", filter.OpGt: ">", filter.OpGte: ">=",
		filter.OpLt: "<", filter.OpLte: "<=", filter.OpLike: "LIKE",
		filter.OpIn: "=",
	}
	for op, want := range cases {
		if got := opToSQL(op); got != want {
			t.Errorf("opToSQL(%v) = %q, want %q", op, got, want)
		}
	}
	if got := opToSQL(filter.FilterOp("bogus")); got != "=" {
		t.Errorf("opToSQL default = %q", got)
	}
}

func TestBuildExistsSubquery_AllRelTypes(t *testing.T) {
	mk := func(rt entity.RelationType) nestedFilter {
		return nestedFilter{
			Relation: entity.Relation{
				Type: rt, Name: "r", Entity: "users", ForeignKey: "author_id",
				Through: "post_tags", LocalKey: "post_id", ForeignKeyTarget: "tag_id",
			},
			Field: "name", Op: filter.OpEq, Value: "x",
		}
	}
	for _, rt := range []entity.RelationType{entity.RelManyToOne, entity.RelHasOne, entity.RelHasMany, entity.RelManyToMany} {
		sql, args := buildExistsSubquery("posts", "id", mk(rt))
		if !strings.HasPrefix(sql, "EXISTS") {
			t.Errorf("rel %v sql = %q", rt, sql)
		}
		if len(args) != 1 {
			t.Errorf("rel %v args = %v", rt, args)
		}
	}
	// Unsafe field name → match-nothing fallback.
	bad := mk(entity.RelManyToOne)
	bad.Field = "name OR 1=1"
	if sql, _ := buildExistsSubquery("posts", "id", bad); sql != "1 = 0" {
		t.Errorf("unsafe field should yield 1=0, got %q", sql)
	}
}

func TestParseNestedFilters_Paths(t *testing.T) {
	ent := covRelEntity()
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"users": entity.Define("users", entity.EntityConfig{
			Name: "users", Table: "users",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
		}.WithTimestamps(false)),
	}}

	// Multi-level path rejected.
	r := httptest.NewRequest("GET", "/?author.team.name=x", nil)
	if _, err := parseNestedFilters(r, ent, reg); err == nil {
		t.Error("expected multi-level rejection")
	}
	// Unknown relation rejected.
	r = httptest.NewRequest("GET", "/?nope.name=x", nil)
	if _, err := parseNestedFilters(r, ent, reg); err == nil {
		t.Error("expected unknown relation rejection")
	}
	// Unsafe field rejected.
	r = httptest.NewRequest("GET", "/?author."+"name%20OR%201=1=x", nil)
	if _, err := parseNestedFilters(r, ent, reg); err == nil {
		t.Error("expected unsafe field rejection")
	}
	// Unknown field on target rejected (registry validates).
	r = httptest.NewRequest("GET", "/?author.unknownfield=x", nil)
	if _, err := parseNestedFilters(r, ent, reg); err == nil {
		t.Error("expected unknown field rejection")
	}
	// _in coalesces into ONE filter carrying all values (emitted as a single
	// IN (...)), so a to-one relation can actually match. Splitting into
	// AND-ed equals made BelongsTo/HasOne unmatchable.
	r = httptest.NewRequest("GET", "/?author.name_in=a,b,c", nil)
	got, err := parseNestedFilters(r, ent, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("_in = %d filters, want 1 (coalesced)", len(got))
	}
	if got[0].Op != filter.OpIn || len(got[0].Values) != 3 {
		t.Errorf("_in values = %v (op %v), want 3 values", got[0].Values, got[0].Op)
	}
	// _like suffix.
	r = httptest.NewRequest("GET", "/?author.name_like=al%25", nil)
	got, err = parseNestedFilters(r, ent, reg)
	if err != nil || len(got) != 1 || got[0].Op != filter.OpLike {
		t.Errorf("_like parse = %v, err=%v", got, err)
	}
}

func TestAuditCtx_RoundTrip(t *testing.T) {
	if AuditRequestFromContext(context.Background()) != nil {
		t.Error("expected nil request when unset")
	}
	if AuditPreImageFromContext(context.Background()) != nil {
		t.Error("expected nil pre-image when unset")
	}
	r := httptest.NewRequest("GET", "/", nil)
	ctx := WithAuditRequest(context.Background(), r)
	if AuditRequestFromContext(ctx) != r {
		t.Error("request round-trip failed")
	}
	// nil request returns ctx unchanged.
	if WithAuditRequest(context.Background(), nil) == nil {
		t.Error("nil request should return ctx")
	}
	pre := map[string]any{"a": 1}
	ctx = WithAuditPreImage(context.Background(), pre)
	if got := AuditPreImageFromContext(ctx); got["a"] != 1 {
		t.Errorf("pre-image round-trip = %v", got)
	}
	if WithAuditPreImage(context.Background(), nil) == nil {
		t.Error("nil pre-image should return ctx")
	}
}

func TestBeforeHookError_Unwrap(t *testing.T) {
	inner := errNotFound
	bhe := &beforeHookError{err: inner}
	if bhe.Unwrap() != inner {
		t.Error("Unwrap mismatch")
	}
	if bhe.Error() != inner.Error() {
		t.Error("Error mismatch")
	}
}

func TestJoinNonEmpty(t *testing.T) {
	got := joinNonEmpty([]string{"a", "", "b", ""}, ",")
	if got != "a,b" {
		t.Errorf("joinNonEmpty = %q", got)
	}
	if joinNonEmpty(nil, ",") != "" {
		t.Error("empty join should be empty")
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(errNotFound) {
		t.Error("errNotFound should be not-found")
	}
	if IsNotFound(nil) {
		t.Error("nil is not not-found")
	}
}
