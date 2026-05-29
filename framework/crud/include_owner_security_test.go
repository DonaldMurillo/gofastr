package crud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestInclude_RelatedTableOwnerScope verifies that ?include=relation on
// a list/get endpoint applies owner-scope to the JOINED entity as well
// as the parent. Attack: alice asks for `/posts?include=comments` and
// receives bob's comments because the related table only filters by
// post_id, ignoring its own user_id column.
//
// Setup: two entities, both with OwnerField="user_id". A post owned by
// alice has comments from both alice and bob. With the fix, alice's
// include=comments must omit bob's comment.
func TestInclude_RelatedTableOwnerScope(t *testing.T) {
	ddl := `
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	title     TEXT
);
CREATE TABLE comments (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	post_id   TEXT NOT NULL,
	body      TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.HasMany("comments", "comments", "post_id"),
			}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "title": "alice post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-alice", "user_id": "alice", "post_id": "p1", "body": "alice comment"},
		{"id": "c-bob", "user_id": "bob", "post_id": "p1", "body": "bob secret"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/posts?include=comments",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "bob secret") {
		t.Errorf("SECURITY: [idor] include=comments returned bob's comment on alice's post. Attack: related-table owner scope missing. Body: %s", body)
	}
	if !strings.Contains(body, "alice comment") {
		t.Errorf("alice's own comment missing from include — owner scope too aggressive? Body: %s", body)
	}
}

// TestInclude_ScopedFilterCannotBypassOwnerScope pins that an
// attacker-supplied scoped filter on the related entity's OwnerField does
// NOT disable cross-table owner scoping. Attack: alice requests
// `/posts?include=comments(user_id=bob)` — the forged predicate must be
// intersected with alice's real owner scope (matching nothing), not treated
// as an opt-out that returns bob's private comment.
func TestInclude_ScopedFilterCannotBypassOwnerScope(t *testing.T) {
	ddl := `
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	title     TEXT
);
CREATE TABLE comments (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	post_id   TEXT NOT NULL,
	body      TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.HasMany("comments", "comments", "post_id"),
			}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "title": "alice post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-alice", "user_id": "alice", "post_id": "p1", "body": "alice comment"},
		{"id": "c-bob", "user_id": "bob", "post_id": "p1", "body": "bob secret"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/posts?include=comments(user_id=bob)",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "bob secret") {
		t.Errorf("SECURITY: [idor] include=comments(user_id=bob) bypassed owner scope and leaked bob's comment. Body: %s", body)
	}
}

// TestInclude_RelatedHiddenFieldNotLeaked pins that a related entity's
// Hidden field is scrubbed from an ?include= response, the same way the
// base read path scrubs it. Attack: declare users.password_hash Hidden,
// then GET /rf_posts?include=author — the eager loader SELECT *'d every
// column of the related row and copied it verbatim, leaking the hash.
func TestInclude_RelatedHiddenFieldNotLeaked(t *testing.T) {
	ddl := `
CREATE TABLE rf_posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	author_id TEXT,
	title     TEXT
);
CREATE TABLE rf_users (
	id            TEXT PRIMARY KEY,
	name          TEXT,
	password_hash TEXT
);
`
	postCfg := makeEntityConfig("rf_posts", "rf_posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				{Name: "author", Type: entity.RelManyToOne, Entity: "rf_users", ForeignKey: "author_id"},
			}
		},
	)
	// rf_users has no OwnerField — the relation is public-by-reference, but
	// password_hash is Hidden and must never surface.
	userCfg := makeEntityConfig("rf_users", "rf_users", "",
		[]schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "password_hash", Type: schema.String, Hidden: true},
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	userEnt := entity.Define(userCfg.Table, userCfg)
	userEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, userEnt)
	ch.Registry = reg

	seedRows(t, db, "rf_users", []map[string]any{
		{"id": "u1", "name": "alice", "password_hash": "super_secret_hash"},
	})
	seedRows(t, db, "rf_posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "author_id": "u1", "title": "alice post"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/rf_posts?include=author",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "super_secret_hash") {
		t.Errorf("SECURITY: [disclosure] include=author leaked related entity's Hidden password_hash. Attack: SELECT * eager-load ignores target Hidden flags. Body: %s", body)
	}
	if !strings.Contains(body, "alice post") {
		t.Errorf("base row missing — test setup wrong. Body: %s", body)
	}
	if !strings.Contains(body, `"author"`) {
		t.Errorf("included author missing — relation not loaded. Body: %s", body)
	}
}

// minimal Registry shim for the include test.
type testRegistry struct {
	mu map[string]*entity.Entity
}

func newTestRegistry(t *testing.T) *testRegistry {
	return &testRegistry{mu: make(map[string]*entity.Entity)}
}
func (r *testRegistry) add(t *testing.T, e *entity.Entity) {
	r.mu[e.GetName()] = e
	r.mu[e.GetTable()] = e
}
func (r *testRegistry) All() map[string]*entity.Entity {
	out := make(map[string]*entity.Entity, len(r.mu))
	for k, v := range r.mu {
		out[k] = v
	}
	return out
}
func (r *testRegistry) AllSorted() []*entity.Entity {
	seen := make(map[*entity.Entity]bool)
	var out []*entity.Entity
	for _, e := range r.mu {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}
func (r *testRegistry) Get(name string) (*entity.Entity, error) {
	if e, ok := r.mu[name]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("entity %q not registered", name)
}
