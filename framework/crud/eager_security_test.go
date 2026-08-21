package crud

import (
	"context"
	"fmt"
	"sort"
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
// rows and never populates Hidden columns, matching the live include
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

	usersEnt := entity.Define("users", entity.EntityConfig{Name: "users", Table: "users", Scope: &entity.ScopeConfig{SoftDelete: true}, Fields: []schema.Field{
		{Name: "name", Type: schema.String},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	},
	})
	commentsEnt := entity.Define("comments", entity.EntityConfig{Name: "comments", Table: "comments", Scope: &entity.ScopeConfig{SoftDelete: true}, Fields: []schema.Field{
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

	// ManyToOne author on p2 references soft-deleted u2, must be absent.
	if _, present := got["p2"]["author"]; present {
		t.Errorf("SECURITY: soft-deleted user u2 resurfaced as p2's author via EagerLoad")
	}
}

// versionedStubRegistry holds several versions of a name and advertises the
// GetVersioned capability, so entity.ResolveTarget can prefer the source's own
// version. Get mirrors the real registry: unversioned wins, a sole version
// wins, anything else is ambiguous.
type versionedStubRegistry struct{ ents []*entity.Entity }

func (r versionedStubRegistry) All() map[string]*entity.Entity {
	out := make(map[string]*entity.Entity, len(r.ents))
	for _, e := range r.ents {
		out[e.GetName()] = e
	}
	return out
}

func (r versionedStubRegistry) AllSorted() []*entity.Entity {
	out := append([]*entity.Entity(nil), r.ents...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetName() != out[j].GetName() {
			return out[i].GetName() < out[j].GetName()
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func (r versionedStubRegistry) Get(name string) (*entity.Entity, error) {
	var matches []*entity.Entity
	for _, e := range r.ents {
		if e.GetName() != name {
			continue
		}
		if e.Version == "" {
			return e, nil
		}
		matches = append(matches, e)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, fmt.Errorf("ambiguous entity %q", name)
}

func (r versionedStubRegistry) GetVersioned(name, version string) (*entity.Entity, error) {
	for _, e := range r.ents {
		if e.GetName() == name && e.Version == version {
			return e, nil
		}
	}
	return nil, fmt.Errorf("entity %q version %q not found", name, version)
}

// versionedComments returns two versions of `comments` sharing one table,
// where only v1 marks legacy_secret Hidden, plus the v1 `posts` parent.
func versionedComments(t *testing.T) (*entity.Entity, versionedStubRegistry) {
	t.Helper()
	field := func(hidden bool) []schema.Field {
		return []schema.Field{
			{Name: "post_id", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "legacy_secret", Type: schema.String, Hidden: hidden},
		}
	}
	v1 := entity.Define("comments", entity.EntityConfig{Fields: field(true)}.WithTimestamps(false))
	v1.Version = "/api/v1"
	v2 := entity.Define("comments", entity.EntityConfig{Fields: field(false)}.WithTimestamps(false))
	v2.Version = "/api/v2"

	posts := entity.Define("posts", entity.EntityConfig{}.WithTimestamps(false))
	posts.Version = "/api/v1"
	return posts, versionedStubRegistry{ents: []*entity.Entity{v1, v2}}
}

// EagerLoad resolved its relation target with registry.Get, which prefers the
// unversioned declaration and errors on ambiguity, so a /api/v1 request
// scrubbed by whichever version Get happened to hand back. Resolution must
// prefer the SOURCE entity's own version (entity.ResolveTarget).
func TestEagerLoadScrubsByRequestVersion(t *testing.T) {
	db := setupDB(t, `CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, legacy_secret TEXT)`)
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "hello", "legacy_secret": "do-not-expose"},
	})

	posts, reg := versionedComments(t)
	rel := entity.HasMany("comments", "comments", "post_id")

	got, err := EagerLoad(context.Background(), db, posts, []entity.Relation{rel}, []string{"p1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	comments, _ := got["p1"]["comments"].([]map[string]any)
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
	if _, leaked := comments[0]["legacy_secret"]; leaked {
		t.Errorf("SECURITY: a v1 eager load exposed a column v1 marks Hidden: %v", comments[0])
	}
}

// An unresolvable target must FAIL, not load with the scrubs off. Discarding
// the resolution error left target nil, which makes hiddenColumns(nil) empty
// and drops the soft-delete predicate, both guards silently disabled on the
// one row set the caller could not vouch for.
func TestEagerLoadFailsOnUnresolvedTarget(t *testing.T) {
	db := setupDB(t, `CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, legacy_secret TEXT)`)
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "hello", "legacy_secret": "do-not-expose"},
	})

	_, reg := versionedComments(t)
	// Unversioned source: neither same-version nor unversioned target exists,
	// and two versions remain, the documented ambiguity error.
	posts := entity.Define("posts", entity.EntityConfig{}.WithTimestamps(false))
	rel := entity.HasMany("comments", "comments", "post_id")

	if _, err := EagerLoad(context.Background(), db, posts, []entity.Relation{rel}, []string{"p1"}, reg); err == nil {
		t.Fatal("SECURITY: EagerLoad loaded rows it could not resolve a schema for")
	}
}

// nilTargetRegistry resolves a name to (nil, nil), no entity, no error.
// A registry implementation is free to do that, and EagerLoad must treat
// it as "schema unknown" rather than "nothing to scrub".
type nilTargetRegistry struct{}

func (nilTargetRegistry) Get(string) (*entity.Entity, error) { return nil, nil }
func (nilTargetRegistry) All() map[string]*entity.Entity     { return nil }
func (nilTargetRegistry) AllSorted() []*entity.Entity        { return nil }

func TestEagerLoadFailsOnNilTarget(t *testing.T) {
	db := setupDB(t, `CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, legacy_secret TEXT)`)
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "hello", "legacy_secret": "do-not-expose"},
	})

	posts := entity.Define("posts", entity.EntityConfig{}.WithTimestamps(false))
	rel := entity.HasMany("comments", "comments", "post_id")

	if _, err := EagerLoad(context.Background(), db, posts, []entity.Relation{rel}, []string{"p1"}, nilTargetRegistry{}); err == nil {
		t.Fatal("EagerLoad served rows for a target that resolved to no entity")
	}
}
