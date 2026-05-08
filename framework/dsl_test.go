package framework

import (
	"reflect"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

func TestParseDSL(t *testing.T) {
	got, err := ParseDSL(`posts.where(status="published", views>=10).include(author).order(created_at DESC).limit(5).after("cursor-1")`)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if got.Entity != "posts" {
		t.Fatalf("entity = %q", got.Entity)
	}
	if len(got.Filters) != 2 {
		t.Fatalf("filters = %#v", got.Filters)
	}
	if got.Filters[0] != (DSLFilter{Field: "status", Operator: "=", Value: "published"}) {
		t.Fatalf("filter 0 = %#v", got.Filters[0])
	}
	if got.Orders[0] != (DSLOrder{Field: "created_at", Direction: "DESC"}) {
		t.Fatalf("order = %#v", got.Orders[0])
	}
	if got.Limit != 5 || got.After != "cursor-1" {
		t.Fatalf("limit/after = %d/%q", got.Limit, got.After)
	}
}

func TestBuildDSLQuery(t *testing.T) {
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "status", Type: schema.String},
			{Name: "views", Type: schema.Int},
			{Name: "author_id", Type: schema.Relation, To: "users"},
			{Name: "created_at", Type: schema.Timestamp},
		},
	})
	registry := NewRegistry()
	if err := registry.Register(entity); err != nil {
		t.Fatal(err)
	}

	qb, err := BuildDSLQuery(registry, `posts.where(status="published", views in [10, 20]).include(author).order(created_at DESC).limit(2)`)
	if err != nil {
		t.Fatalf("BuildDSLQuery: %v", err)
	}
	sql, args := qb.Build()
	wantSQL := "SELECT id, status, views, author_id, created_at, updated_at FROM posts WHERE status = $1 AND views IN ($2, $3) ORDER BY created_at DESC LIMIT $4"
	if sql != wantSQL {
		t.Fatalf("sql:\n got: %s\nwant: %s", sql, wantSQL)
	}
	if !reflect.DeepEqual(args, []any{"published", 10, 20, 2}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestBuildDSLQueryRejectsUnknownField(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Define("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildDSLQuery(registry, `posts.where(missing="x")`); err == nil {
		t.Fatal("expected unknown field error")
	}
}
