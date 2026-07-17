package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// These tests pin issue #66: an Include / EagerLoad over a nullable
// foreign key (e.g. work_items.milestone_id) must NOT error on a NULL FK.
// Instead the parent row comes back with the optional relation absent.
//
// Both BelongsTo loaders historically scanned the FK into a plain string,
// so a NULL FK produced:
//
//	sql: Scan error ... converting NULL to string is unsupported
//
// and the whole eager load (and the request that triggered it) failed.

// nullFKWorld builds a parent table with a nullable author_id and a
// target authors table, seeding one parent that points at an author and
// one orphan whose author_id is NULL.
func nullFKWorld(t *testing.T) (*sql.DB, *entity.Entity, *entity.Entity, *stubRegistry) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, author_id TEXT, title TEXT)`,
		`CREATE TABLE authors (id TEXT PRIMARY KEY, name TEXT)`,
	)
	seedRows(t, db, "authors", []map[string]any{
		{"id": "a1", "name": "alice"},
	})
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "author_id": "a1", "title": "has author"},
		{"id": "p2", "author_id": nil, "title": "orphan"},
	})
	authorsEnt := entity.Define("authors", entity.EntityConfig{
		Name: "authors", Table: "authors",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	})
	postsEnt := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{
			{Name: "author_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "authors", "author_id"),
		},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"authors": authorsEnt, "posts": postsEnt}}
	return db, postsEnt, authorsEnt, &reg
}

// TestEagerLoadBelongsToNullFK covers the legacy exported EagerLoad helper
// (eager.go → eagerLoadBelongsTo): a NULL FK must yield the parent row
// with the relation absent, not a scan error.
func TestEagerLoadBelongsToNullFK(t *testing.T) {
	db, postsEnt, _, reg := nullFKWorld(t)

	rels := []entity.Relation{entity.BelongsTo("author", "authors", "author_id")}
	got, err := EagerLoad(context.Background(), db, postsEnt, rels, []string{"p1", "p2"}, *reg)
	if err != nil {
		t.Fatalf("EagerLoad with NULL FK errored: %v", err)
	}
	// p1 still resolves its author.
	author, _ := got["p1"]["author"].(map[string]any)
	if author == nil || author["name"] != "alice" {
		t.Fatalf("p1 author = %v, want alice", author)
	}
	// p2's author_id is NULL → relation must be absent, no error.
	if _, present := got["p2"]["author"]; present {
		t.Errorf("NULL FK should leave author absent; got %v", got["p2"]["author"])
	}
}

// TestIncludeBelongsToNullFK covers the live include path
// (eager_filtered.go → loadBelongsToFiltered) end-to-end via the HTTP
// List handler: ?include=author over a row with a NULL author_id.
func TestIncludeBelongsToNullFK(t *testing.T) {
	db, postsEnt, _, reg := nullFKWorld(t)
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg

	req := httptest.NewRequest(http.MethodGet, "/posts?include=author", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":"p2"`) {
		t.Fatalf("orphan row p2 dropped from response: %s", body)
	}
	if strings.Contains(body, "converting NULL to string") {
		t.Fatalf("NULL FK scan error leaked into response: %s", body)
	}
}
