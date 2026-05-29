package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// stubRegistry is a minimal entity.Registry for resolving relation targets.
type stubRegistry struct{ byName map[string]*entity.Entity }

func (s stubRegistry) All() map[string]*entity.Entity { return s.byName }
func (s stubRegistry) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(s.byName))
	for _, e := range s.byName {
		out = append(out, e)
	}
	return out
}
func (s stubRegistry) Get(name string) (*entity.Entity, error) {
	if e, ok := s.byName[name]; ok {
		return e, nil
	}
	return nil, errNoSuchStubEntity
}

var errNoSuchStubEntity = &stubErr{}

type stubErr struct{}

func (*stubErr) Error() string { return "no such entity" }

// TestEagerLoadScrubsSoftDeleteAndHidden asserts the legacy exported
// EagerLoad helper, when given a registry, excludes soft-deleted target
// rows and never populates Hidden columns — matching the live include
// path (eager_filtered.go). The same scrubbing must hold for HasMany
// (child holds FK) and ManyToOne (parent holds FK) shapes.
func TestEagerLoadScrubsSoftDeleteAndHidden(t *testing.T) {
	ctx := context.Background()

	// users: target of a ManyToOne, has a Hidden password_hash + soft delete.
	// posts: parent. comments: target of a HasMany, soft-deletable.
	db := setupDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT, password_hash TEXT, deleted_at TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, author_id TEXT)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, secret TEXT, deleted_at TEXT)`,
	)

	seedRows(t, db, "users", []map[string]any{
		{"id": "u1", "name": "alice", "password_hash": "HASHSECRET", "deleted_at": nil},
		{"id": "u2", "name": "ghost", "password_hash": "HASH2", "deleted_at": "2026-01-01"},
	})
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "author_id": "u1"},
		{"id": "p2", "author_id": "u2"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "live", "secret": "SHH", "deleted_at": nil},
		{"id": "c2", "post_id": "p1", "body": "trashed", "secret": "SHH2", "deleted_at": "2026-01-01"},
	})

	usersEnt := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users", SoftDelete: true,
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "password_hash", Type: schema.String, Hidden: true},
		},
	})
	commentsEnt := entity.Define("comments", entity.EntityConfig{
		Name: "comments", Table: "comments", SoftDelete: true,
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "secret", Type: schema.String, Hidden: true},
		},
	})
	postsEnt := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{{Name: "author_id", Type: schema.String}},
	})

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"users": usersEnt, "comments": commentsEnt, "posts": postsEnt,
	}}

	rels := []entity.Relation{
		entity.HasMany("comments", "comments", "post_id"),
		entity.BelongsTo("author", "users", "author_id"),
	}

	got, err := EagerLoad(ctx, db, postsEnt, rels, []string{"p1", "p2"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}

	// HasMany comments on p1: only the live comment, secret scrubbed.
	comments, _ := got["p1"]["comments"].([]map[string]any)
	if len(comments) != 1 {
		t.Fatalf("SECURITY: soft-deleted comment leaked via EagerLoad: got %d comments, want 1 (%v)", len(comments), comments)
	}
	if _, leaked := comments[0]["secret"]; leaked {
		t.Errorf("SECURITY: Hidden column 'secret' leaked via EagerLoad: %v", comments[0])
	}
	if comments[0]["body"] != "live" {
		t.Errorf("expected live comment, got %v", comments[0]["body"])
	}

	// ManyToOne author on p1: u1 is live, password_hash scrubbed.
	author, _ := got["p1"]["author"].(map[string]any)
	if author == nil {
		t.Fatalf("expected author for p1")
	}
	if _, leaked := author["password_hash"]; leaked {
		t.Errorf("SECURITY: Hidden column 'password_hash' leaked via EagerLoad: %v", author)
	}

	// ManyToOne author on p2 references soft-deleted u2 — must be absent.
	if _, present := got["p2"]["author"]; present {
		t.Errorf("SECURITY: soft-deleted user u2 resurfaced as p2's author via EagerLoad")
	}
}
