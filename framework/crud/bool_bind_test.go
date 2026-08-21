package crud

import (
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Every place a Bool-column filter value reaches a bind must coerce it the
// way filter.ParseFiltersValues does. SQLite stores Bool as INTEGER and
// never matches the TEXT 'true'/'false' spellings. These tests pin the
// three crud-side binders: nested relation filters, scoped include
// filters, and the eager-load filter clause.

func boolBindFixtures() (*entity.Entity, stubRegistry) {
	users := entity.Define("users", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "active", Type: schema.Bool},
			{Name: "name", Type: schema.String},
		},
	})
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
		},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "users": users}}
	return posts, reg
}

func TestNestedBoolFilterBindsBool(t *testing.T) {
	posts, reg := boolBindFixtures()
	q := url.Values{"author.active": {"true"}}
	nfs, err := parseNestedFiltersValues(q, posts, reg)
	if err != nil {
		t.Fatalf("parseNestedFiltersValues: %v", err)
	}
	if len(nfs) != 1 {
		t.Fatalf("filters = %v, want 1", nfs)
	}
	_, args := buildExistsSubquery("posts", "id", nfs[0])
	if len(args) != 1 {
		t.Fatalf("args = %v, want 1", args)
	}
	if b, ok := args[0].(bool); !ok || !b {
		t.Fatalf("args[0] = %#v, want bool true (raw string binds TEXT against INTEGER on SQLite)", args[0])
	}
}

func TestNestedBoolInFilterBindsBools(t *testing.T) {
	posts, reg := boolBindFixtures()
	q := url.Values{"author.active_in": {"true,false"}}
	nfs, err := parseNestedFiltersValues(q, posts, reg)
	if err != nil {
		t.Fatalf("parseNestedFiltersValues: %v", err)
	}
	if len(nfs) != 1 {
		t.Fatalf("filters = %v, want 1", nfs)
	}
	_, args := buildExistsSubquery("posts", "id", nfs[0])
	if len(args) != 2 || args[0] != true || args[1] != false {
		t.Fatalf("args = %#v, want [true false]", args)
	}
}

func TestNestedNonBoolFilterStaysString(t *testing.T) {
	posts, reg := boolBindFixtures()
	q := url.Values{"author.name": {"true"}}
	nfs, err := parseNestedFiltersValues(q, posts, reg)
	if err != nil {
		t.Fatalf("parseNestedFiltersValues: %v", err)
	}
	_, args := buildExistsSubquery("posts", "id", nfs[0])
	if len(args) != 1 || args[0] != "true" {
		t.Fatalf("args = %#v, want [\"true\"] (string, not coerced)", args)
	}
}

func TestScopedIncludeBoolFilterBindsBool(t *testing.T) {
	fields := []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "published", Type: schema.Bool},
	}
	fs, err := parseScopedFilters("published=true", fields, "posts")
	if err != nil {
		t.Fatalf("parseScopedFilters: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("filters = %v, want 1", fs)
	}
	if got := fs[0].BindValue(); got != true {
		t.Fatalf("BindValue() = %#v, want bool true", got)
	}
	// And the rendered clause must bind what BindValue says.
	_, args := renderFilterClause(fs, "posts", 1)
	if len(args) != 1 || args[0] != true {
		t.Fatalf("renderFilterClause args = %#v, want [true]", args)
	}
}

func TestScopedIncludeBoolInFilterBindsBools(t *testing.T) {
	fields := []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "published", Type: schema.Bool},
	}
	fs, err := parseScopedFilters("published_in=true|false", fields, "posts")
	if err != nil {
		t.Fatalf("parseScopedFilters: %v", err)
	}
	_, args := renderFilterClause(fs, "posts", 1)
	if len(args) != 2 || args[0] != true || args[1] != false {
		t.Fatalf("renderFilterClause args = %#v, want [true false]", args)
	}
}
