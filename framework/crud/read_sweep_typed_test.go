package crud

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type sweepItem struct {
	ID  string `json:"id"`
	Seq int    `json:"seq"`
}

// An offset page of a typed query is a slice of a total order: the key
// is the last ORDER BY column.
func TestTypedOffsetOrdersByKey(t *testing.T) {
	ch, _ := covItems(t, nil, 0)
	qb, err := NewTypedQuery[sweepItem](ch).Offset(1).buildSelect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := qb.Build()
	if !strings.Contains(sql, "ORDER BY") || !strings.Contains(sql[strings.Index(sql, "ORDER BY"):], "id") {
		t.Fatalf("offset query has no key order: %s", sql)
	}
}

// MarshalEntity keeps an integer above 2^53 exact.
func TestMarshalEntityKeepsHugeInt(t *testing.T) {
	type big struct {
		N int64 `json:"n"`
	}
	m, err := MarshalEntity(big{N: 9007199254740993})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(m["n"]); got != "9007199254740993" {
		t.Fatalf("n = %s (%T), want 9007199254740993", got, m["n"])
	}
}

// UpdateAll refuses a float64 that is not an exact Int value, as the HTTP
// update path does, instead of rounding it into every matched row.
func TestUpdateAllRefusesInexactInt(t *testing.T) {
	ch, _ := covItems(t, nil, 1)
	if _, err := NewTypedQuery[sweepItem](ch).UpdateAll(context.Background(), map[string]any{"seq": float64(1 << 60)}); err == nil {
		t.Fatal("UpdateAll wrote a float64 above 2^53 into an Int column")
	}
	if _, err := NewTypedQuery[sweepItem](ch).UpdateAll(context.Background(), map[string]any{"seq": float64(7)}); err != nil {
		t.Fatalf("control: an exact float64 is refused: %v", err)
	}
}

// A typed query hands its includes the caller's ctx, so under
// WithReadHooks an included row is masked like its parent.
func TestTypedIncludeKeepsReadHooks(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, author_id TEXT)`,
	)
	seedRows(t, db, "users", []map[string]any{{"id": "u1", "name": "secret"}})
	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "author_id": "u1"}})
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users",
		Fields: []schema.Field{{Name: "name", Type: schema.String, Mask: func(context.Context, any) any { return "***" }}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:    []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"users": users, "posts": posts}}
	type post struct {
		Author map[string]any `json:"author"`
	}
	ctx := WithReadHooks(handler.SetUser(context.Background(), &testUser{id: "u1"}))
	rows, err := NewTypedQuery[post](ch).Include("author").Find(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Author["name"] != "***" {
		t.Fatalf("included author = %v, want name masked", rows[0].Author)
	}
}
