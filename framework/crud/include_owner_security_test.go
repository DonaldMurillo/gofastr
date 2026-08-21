package crud

import (
	"fmt"
	"maps"
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
// `/posts?include=comments(user_id=bob)`, the forged predicate must be
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
// then GET /rf_posts?include=author, the eager loader SELECT *'d every
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
	// rf_users has no OwnerField, the relation is public-by-reference, but
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

// TestIncludeUnregisteredTargetFails pins the property that an ?include=
// whose relation target cannot be resolved in the registry is REFUSED,
// never eager-loaded with `SELECT *` and served unscrubbed.
//
// Attack: the blueprint wires auth.NewEntityUserStore over a self-migrated
// `auth_users` table that is never registered via app.Entity. A generated
// entity that declares BelongsTo("author", "auth_users", …) plus a writable
// author FK lets any caller point the FK at another user's row and read it
// back via ?include=author. With no Target the eager loader loses every
// guard keyed off it: hidden-column scrub, owner scope, tenant scope,
// soft-delete filter, and the scoped-filter field allow-list.
//
// The property must hold at every include surface, so this loops over them
// rather than repeating case shapes: HTTP List, HTTP List with ?cursor=,
// HTTP Get, and the in-process GetOne / ListAll entry points. All five
// funnel through parseIncludeTree, which is where the refusal belongs.
func TestIncludeUnregisteredTargetFails(t *testing.T) {
	ddl := `
CREATE TABLE ur_posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	author_id TEXT,
	title     TEXT
);
CREATE TABLE ur_users (
	id            TEXT PRIMARY KEY,
	name          TEXT,
	password_hash TEXT
);
`
	postCfg := makeEntityConfig("ur_posts", "ur_posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				{Name: "author", Type: entity.RelManyToOne, Entity: "ur_users", ForeignKey: "author_id"},
			}
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	// ur_users is deliberately NOT registered, this is the self-migrated
	// auth table shape the blueprint emits.
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	ch.Registry = reg

	seedRows(t, db, "ur_users", []map[string]any{
		{"id": "u-bob", "name": "bob", "password_hash": "bob_secret_hash"},
	})
	seedRows(t, db, "ur_posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "author_id": "u-bob", "title": "alice post"},
	})

	httpSurfaces := []struct {
		name string
		path string
		call func(*CrudHandler) http.HandlerFunc
	}{
		{"list", "/ur_posts?include=author", (*CrudHandler).List},
		{"list+cursor", "/ur_posts?include=author&cursor=", (*CrudHandler).List},
		{"get", "/ur_posts/p1?include=author", (*CrudHandler).Get},
	}
	for _, s := range httpSurfaces {
		t.Run(s.name, func(t *testing.T) {
			req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: s.path, UserID: "alice"})
			if s.name == "get" {
				req.SetPathValue("id", "p1")
			}
			rr := httptest.NewRecorder()
			s.call(ch)(rr, req)

			if rr.Code == http.StatusOK {
				t.Errorf("SECURITY: [disclosure] %s served an include with an unresolvable target instead of refusing it. Body: %s", s.name, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "bob_secret_hash") {
				t.Errorf("SECURITY: [disclosure] %s leaked the unregistered target's password_hash. Body: %s", s.name, rr.Body.String())
			}
		})
	}

	t.Run("GetOne", func(t *testing.T) {
		ctx := withTestUserCtx("alice")
		got, err := ch.GetOne(ctx, "p1", []string{"author"})
		if err == nil {
			t.Errorf("SECURITY: [disclosure] GetOne eager-loaded an unresolvable include target instead of erroring. Got: %v", got)
		}
	})
	t.Run("ListAll", func(t *testing.T) {
		ctx := withTestUserCtx("alice")
		got, err := ch.ListAll(ctx, ListOptions{Includes: []string{"author"}})
		if err == nil {
			t.Errorf("SECURITY: [disclosure] ListAll eager-loaded an unresolvable include target instead of erroring. Got: %v", got)
		}
	})
}

// TestIncludeResolvesByNameNotTable pins the second trigger of the same
// root cause: Relation.Entity is the registry key (the entity NAME) while
// the eager-load SELECT must target the entity's TABLE. An entity whose
// Name differs from its Table used to nil one lookup or query the wrong
// relation, silently dropping the same five guards.
func TestIncludeResolvesByNameNotTable(t *testing.T) {
	ddl := `
CREATE TABLE nt_posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	author_id TEXT,
	title     TEXT
);
CREATE TABLE nt_user_rows (
	id            TEXT PRIMARY KEY,
	name          TEXT,
	password_hash TEXT
);
`
	postCfg := makeEntityConfig("nt_posts", "nt_posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				// Entity names the registered NAME ("nt_users"); the rows
				// live in table "nt_user_rows".
				{Name: "author", Type: entity.RelManyToOne, Entity: "nt_users", ForeignKey: "author_id"},
			}
		},
	)
	userCfg := makeEntityConfig("nt_users", "nt_user_rows", "",
		[]schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "password_hash", Type: schema.String, Hidden: true},
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	userEnt := entity.Define(userCfg.Name, userCfg)
	userEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.addByName(ch.Entity)
	reg.addByName(userEnt)
	ch.Registry = reg

	seedRows(t, db, "nt_user_rows", []map[string]any{
		{"id": "u1", "name": "alice", "password_hash": "name_table_secret"},
	})
	seedRows(t, db, "nt_posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "author_id": "u1", "title": "alice post"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/nt_posts?include=author", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s) — a registered target whose Name != Table must still load", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "name_table_secret") {
		t.Errorf("SECURITY: [disclosure] include on a Name!=Table entity leaked the Hidden column. Body: %s", body)
	}
	if !strings.Contains(body, `"name":"alice"`) && !strings.Contains(body, `"name": "alice"`) {
		t.Errorf("include did not load the target through its Table — relation resolved to the wrong table. Body: %s", body)
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

// addByName mirrors framework.Registry, which keys by entity Name only.
// The dual-key add() above hides the Name-vs-Table confusion that
// TestIncludeResolvesByNameNotTable exists to catch.
func (r *testRegistry) addByName(e *entity.Entity) {
	r.mu[e.GetName()] = e
}
func (r *testRegistry) All() map[string]*entity.Entity {
	out := make(map[string]*entity.Entity, len(r.mu))
	maps.Copy(out, r.mu)
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

// TestNestedLikeIsLiteralSubstring pins that `?rel.field_like=` means the
// same thing at every depth.
//
// Nested `_like` used to pass the caller's value through as a RAW LIKE
// pattern while top-level `_like` escaped it and wrapped it in wildcards,
// the same query parameter spelled the same way meaning two different
// things depending on whether a dot appeared in it. That is a footgun
// before it is a vulnerability: a caller filtering on a value that
// happens to contain `%` or `_` (an email, a path, a SQL-ish string)
// silently matched far more rows than they asked for, and an unescaped
// `%` is a full-table-scan primitive.
//
// One operator, one meaning: literal substring, wildcards escaped.
func TestNestedLikeIsLiteralSubstring(t *testing.T) {
	ddl := `
CREATE TABLE nl_posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	author_id TEXT,
	title     TEXT
);
CREATE TABLE nl_users (
	id   TEXT PRIMARY KEY,
	name TEXT
);
`
	postCfg := makeEntityConfig("nl_posts", "nl_posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				{Name: "author", Type: entity.RelManyToOne, Entity: "nl_users", ForeignKey: "author_id"},
			}
		},
	)
	userCfg := makeEntityConfig("nl_users", "nl_users", "",
		[]schema.Field{{Name: "name", Type: schema.String}},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	userEnt := entity.Define(userCfg.Name, userCfg)
	userEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.addByName(ch.Entity)
	reg.addByName(userEnt)
	ch.Registry = reg

	seedRows(t, db, "nl_users", []map[string]any{
		{"id": "u1", "name": "100% cotton"},
		{"id": "u2", "name": "alice"},
	})
	seedRows(t, db, "nl_posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "author_id": "u1", "title": "literal-match"},
		{"id": "p2", "user_id": "alice", "author_id": "u2", "title": "wildcard-match"},
	})

	// "100%" must match the user literally named "100% cotton" and NOT
	// act as a prefix wildcard.
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/nl_posts?author.name_like=100%25", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("nested like returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "literal-match") {
		t.Errorf("nested _like did not match the literal substring %q. Body: %s", "100%", body)
	}
	if strings.Contains(body, "wildcard-match") {
		t.Errorf("SECURITY: [dos] nested _like treated %q as a wildcard pattern and matched every row — top-level _like escapes it. Body: %s", "100%", body)
	}

	// A bare "%" must match nothing rather than every row.
	req = makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/nl_posts?author.name_like=%25", UserID: "alice"})
	rr = httptest.NewRecorder()
	ch.List()(rr, req)
	if strings.Contains(rr.Body.String(), "wildcard-match") {
		t.Errorf("SECURITY: [dos] a bare %q matched every row — the wildcard was not escaped. Body: %s", "%", rr.Body.String())
	}
}
