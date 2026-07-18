package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestPureSQLiteManyToManyNestedPredicate(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, ddl := range []string{
		`CREATE TABLE m2m_posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`,
		`CREATE TABLE m2m_tags (id TEXT PRIMARY KEY, label TEXT NOT NULL)`,
		`CREATE TABLE m2m_post_tags (post_id TEXT NOT NULL, tag_id TEXT NOT NULL, PRIMARY KEY (post_id, tag_id))`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	for _, insert := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO m2m_posts (id, title) VALUES ($1, $2)", []any{"p1", "tagged"}},
		{"INSERT INTO m2m_posts (id, title) VALUES ($1, $2)", []any{"p2", "plain"}},
		{"INSERT INTO m2m_tags (id, label) VALUES ($1, $2)", []any{"t1", "go"}},
		{"INSERT INTO m2m_post_tags (post_id, tag_id) VALUES ($1, $2)", []any{"p1", "t1"}},
	} {
		if _, err := db.Exec(insert.query, insert.args...); err != nil {
			t.Fatalf("seed %q: %v", insert.query, err)
		}
	}

	tags := entity.Define("m2m_tags", entity.EntityConfig{
		Name: "m2m_tags", Table: "m2m_tags",
		Fields: []schema.Field{{Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	posts := entity.Define("m2m_posts", entity.EntityConfig{
		Name: "m2m_posts", Table: "m2m_posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			entity.ManyToMany("tags", "m2m_tags", "m2m_post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	posts.SetDB(db)
	registry := stubRegistry{byName: map[string]*entity.Entity{
		"m2m_posts": posts,
		"m2m_tags":  tags,
	}}
	handler := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	handler.Registry = registry

	rows, err := handler.ListAll(context.Background(), ListOptions{
		NestedFilters: []NestedFilter{
			{Relation: "tags", Field: "label", Op: filter.OpEq, Value: "go"},
		},
	})
	if err != nil {
		t.Fatalf("many-to-many nested filter: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "p1" {
		t.Fatalf("many-to-many rows = %v, want only p1", rows)
	}
}
