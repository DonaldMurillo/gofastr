package crud

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Reads inside App.InTx run on its transaction, so they see its own
// uncommitted writes. They ran on the pool: on Postgres they missed the
// row the transaction had just written, and on a one-connection pool
// (this test's SQLite) they waited on the transaction until the
// deadline. The bulk update's foreign-key check read the pool too and
// refused a parent written in the same transaction.
func TestReadsInTxSeeOwnWrites(t *testing.T) {
	installSecurityOwnerExtractor(t)
	conn := setupDB(t,
		`CREATE TABLE authors (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`,
		`CREATE TABLE books (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, author_id TEXT, pages INTEGER)`)
	authors := entity.Define("authors", makeEntityConfig("authors", "authors", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String},
		{Name: "name", Type: schema.String},
	}))
	books := entity.Define("books", makeEntityConfig("books", "books", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String},
		{Name: "author_id", Type: schema.String},
		{Name: "pages", Type: schema.Int},
	}, func(c *entity.EntityConfig) {
		c.Relations = []entity.Relation{entity.BelongsTo("author", "authors", "author_id")}
	}))
	reg := stubRegistry{byName: map[string]*entity.Entity{"authors": authors, "books": books}}
	chA, chB := NewCrudHandler(authors, conn), NewCrudHandler(books, conn)
	chA.Registry, chB.Registry = reg, reg

	user := handler.SetUser(context.Background(), &testUser{id: "alice"})
	old, err := chB.CreateOne(user, map[string]any{"pages": 1})
	if err != nil {
		t.Fatal(err)
	}

	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	ctx, cancel := context.WithTimeout(db.WithTx(user, tx), 2*time.Second)
	defer cancel()
	a, err := chA.CreateOne(ctx, map[string]any{"name": "Ann"})
	if err != nil {
		t.Fatal(err)
	}
	aid, _ := a["id"].(string)
	b, err := chB.CreateOne(ctx, map[string]any{"pages": 5, "author_id": aid})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := chA.GetOne(ctx, aid, nil); err != nil {
		t.Errorf("GetOne: %v", err)
	}
	if rows, err := chA.ListAll(ctx, ListOptions{}); err != nil || len(rows) != 1 {
		t.Errorf("ListAll = %d rows, %v; want 1", len(rows), err)
	}
	if n, err := chA.CountAll(ctx, ListOptions{}); err != nil || n != 1 {
		t.Errorf("CountAll = %d, %v; want 1", n, err)
	}
	if sum, err := chB.SumAll(ctx, "pages", ListOptions{}); err != nil || sum != "6" {
		t.Errorf("SumAll = %q, %v; want 6", sum, err)
	}
	got, err := chB.GetOne(ctx, b["id"].(string), []string{"author"})
	if err != nil {
		t.Errorf("GetOne with include: %v", err)
	} else if inc, _ := got["author"].(map[string]any); inc == nil || inc["name"] != "Ann" {
		t.Errorf("included author = %v, want Ann", got["author"])
	}
	type bookRow struct {
		ID string `json:"id"`
	}
	if n, err := NewTypedQuery[bookRow](chB).
		Where(entity.NewStringColumn("id").Eq(old["id"].(string))).
		UpdateAll(ctx, map[string]any{"author_id": aid}); err != nil || n != 1 {
		t.Errorf("UpdateAll to an author written in the tx = %d, %v; want 1", n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
