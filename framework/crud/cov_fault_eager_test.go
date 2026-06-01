package crud

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covFaultRelWorld builds the same posts/users/comments/tags world as
// covRelWorld but over a fault-injectable DB so the SECOND query and
// rows.Err iteration branches in the eager loaders can be exercised (the
// missing-table world only fails the FIRST query).
func covFaultRelWorld(t *testing.T) (*CrudHandler, *sql.DB, stubRegistry) {
	t.Helper()
	db := covSetupFaultDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT, password_hash TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
		`CREATE TABLE tags (id TEXT PRIMARY KEY, label TEXT)`,
		`CREATE TABLE post_tags (post_id TEXT, tag_id TEXT)`,
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, post_id TEXT, bio TEXT)`,
	)
	seedRows(t, db, "users", []map[string]any{{"id": "u1", "name": "alice", "password_hash": "SECRET"}})
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "title": "first", "author_id": "u1"},
		{"id": "p2", "title": "second", "author_id": "u1"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "nice"},
		{"id": "c2", "post_id": "p1", "body": "ok"},
	})
	seedRows(t, db, "tags", []map[string]any{{"id": "t1", "label": "go"}, {"id": "t2", "label": "db"}})
	seedRows(t, db, "post_tags", []map[string]any{
		{"post_id": "p1", "tag_id": "t1"}, {"post_id": "p1", "tag_id": "t2"},
	})
	seedRows(t, db, "profiles", []map[string]any{{"id": "pr1", "post_id": "p1", "bio": "the bio"}})

	usersEnt := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users",
		Fields: []schema.Field{{Name: "name", Type: schema.String}, {Name: "password_hash", Type: schema.String, Hidden: true}},
	}.WithTimestamps(false))
	commentsEnt := entity.Define("comments", entity.EntityConfig{
		Name: "comments", Table: "comments",
		Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	tagsEnt := entity.Define("tags", entity.EntityConfig{
		Name: "tags", Table: "tags", Fields: []schema.Field{{Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	profilesEnt := entity.Define("profiles", entity.EntityConfig{
		Name: "profiles", Table: "profiles",
		Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "bio", Type: schema.String}},
	}.WithTimestamps(false))
	postsEnt := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
			entity.HasMany("comments", "comments", "post_id"),
			entity.HasOne("profile", "profiles", "post_id"),
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"users": usersEnt, "comments": commentsEnt, "tags": tagsEnt,
		"profiles": profilesEnt, "posts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	return ch, db, reg
}

// --- eager.go (unfiltered EagerLoad) DB-error branches ---

func TestEagerHasMany_NextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.HasMany("comments", "comments", "post_id")
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"comments\"" })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("hasMany rows.Err = %v, want injected", err)
	}
}

func TestEagerBelongsTo_SrcNextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.BelongsTo("author", "users", "author_id")
	// Source query selects from posts; fail iteration of it.
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"posts\"" })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("belongsTo src rows.Err = %v, want injected", err)
	}
}

func TestEagerBelongsTo_TgtQueryErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.BelongsTo("author", "users", "author_id")
	// Source query (posts) runs; target query (users) fails.
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM \"users\"" })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("belongsTo tgt query = %v, want injected", err)
	}
}

func TestEagerBelongsTo_TgtNextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.BelongsTo("author", "users", "author_id")
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"users\"" })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("belongsTo tgt rows.Err = %v, want injected", err)
	}
}

func TestEagerManyToMany_NextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	m2m := entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")
	m2m.ForeignKey = "tag_id"
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"tags\"" })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{m2m}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("m2m rows.Err = %v, want injected", err)
	}
}

// --- eager_filtered.go DB-error branches (via List include path) ---

func TestFilteredHasMany_NextErr(t *testing.T) {
	ch, db, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"comments\"" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"comments"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered hasMany rows.Err = %v, want injected", err)
	}
	_ = db
}

func belongsToAuthorNode() *IncludeNode {
	return &IncludeNode{Relation: entity.BelongsTo("author", "users", "author_id")}
}

func TestFilteredBelongsTo_SrcQueryErr(t *testing.T) {
	_, db, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM \"posts\"" })
	err := loadIncludeNode(context.Background(), db, "posts", "id", belongsToAuthorNode(), []string{"p1"}, newResult("p1"))
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo src query = %v, want injected", err)
	}
}

func TestFilteredBelongsTo_SrcNextErr(t *testing.T) {
	_, db, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"posts\"" })
	err := loadIncludeNode(context.Background(), db, "posts", "id", belongsToAuthorNode(), []string{"p1"}, newResult("p1"))
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo src rows.Err = %v, want injected", err)
	}
}

func TestFilteredBelongsTo_TgtQueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM \"users\"" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"author"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo tgt query = %v, want injected", err)
	}
}

func TestFilteredBelongsTo_TgtNextErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM \"users\"" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"author"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo tgt rows.Err = %v, want injected", err)
	}
}

func TestFilteredManyToMany_QueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "__parent_id" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"tags"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered m2m query = %v, want injected", err)
	}
}

func TestFilteredManyToMany_NextErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "__parent_id" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"tags"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered m2m rows.Err = %v, want injected", err)
	}
}

func TestFilteredHasMany_QueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM \"comments\"" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"comments"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered hasMany query = %v, want injected", err)
	}
}
