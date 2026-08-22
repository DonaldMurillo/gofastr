package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// `?rel.field=` does not return the related row, so it is not a disclosure the
// way ?include= was. It is a count oracle: the EXISTS clause used to count
// every row in the target table, so the PARENT's row count moved with the
// guessed value and a signed-in owner could confirm any value in any other
// owner's column, one guess at a time.
//
// The old fix refused the shape outright for every owner-scoped target, which
// closed the oracle and took the ordinary case with it — an owner could not
// filter their OWN rows through a relation either. The subquery now carries the
// caller's owner predicate instead, so it counts exactly the rows that owner
// could already list through the target's own route.
//
// Both halves are asserted here against a real database, because a scope that
// is attached to a struct but lost on the way to SQL passes every unit test
// above it and leaks anyway.
func TestNestedFilterCountsOnlyTheCallersOwnRows(t *testing.T) {
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
			c.Relations = []entity.Relation{entity.HasMany("comments", "comments", "post_id")}
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

	// One post owned by alice, carrying a comment from each of them. Alice may
	// read the post; the question is which of its comments she can measure.
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "title": "alice post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-alice", "user_id": "alice", "post_id": "p1", "body": "alice comment"},
		{"id": "c-bob", "user_id": "bob", "post_id": "p1", "body": "bob secret"},
	})

	listCount := func(t *testing.T, query string) int {
		t.Helper()
		req := makeRequest(t, RequestOpts{
			Method: http.MethodGet,
			Path:   "/posts?" + query,
			UserID: "alice",
		})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /posts?%s = %d (body=%s)", query, rr.Code, rr.Body.String())
		}
		var env struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("GET /posts?%s: %v (body=%s)", query, err, rr.Body.String())
		}
		return len(env.Data)
	}

	// The capability the blanket refusal removed: alice filtering her post by
	// her own comment.
	if got := listCount(t, "comments.body=alice+comment"); got != 1 {
		t.Errorf("alice could not filter her own post by her own comment: got %d rows, want 1", got)
	}

	// The oracle. A guess that hits bob's comment must be indistinguishable
	// from a guess that hits nothing at all — same status, same row count.
	hit := listCount(t, "comments.body=bob+secret")
	miss := listCount(t, "comments.body=no+such+comment+anywhere")
	if hit != miss {
		t.Errorf("SECURITY: [oracle] guessing bob's comment body returned %d rows and a nonexistent value returned %d — "+
			"the row count discloses another owner's column value one guess at a time", hit, miss)
	}
	if hit != 0 {
		t.Errorf("a filter matching only another owner's comment returned %d rows, want 0", hit)
	}

	// Character-by-character recovery is the practical form of the attack, and
	// _like is the operator that makes it cheap. A prefix of bob's body must
	// read the same as a prefix of nothing.
	likeHit := listCount(t, "comments.body_like=bob")
	likeMiss := listCount(t, "comments.body_like=zzz")
	if likeHit != likeMiss {
		t.Errorf("SECURITY: [oracle] a _like prefix of bob's comment returned %d rows and an absent prefix returned %d", likeHit, likeMiss)
	}
	// …while alice's own prefix still works, or the operator is useless to her.
	if got := listCount(t, "comments.body_like=alice"); got != 1 {
		t.Errorf("alice's own _like prefix returned %d rows, want 1", got)
	}

	// _in coalesces into one IN (…) and takes a separate branch in
	// buildExistsSubquery, so it needs its own proof that the scope reaches it.
	inHit := listCount(t, "comments.body_in=bob+secret")
	if inHit != 0 {
		t.Errorf("SECURITY: [oracle] the _in branch counted another owner's row: got %d rows, want 0", inHit)
	}
	if got := listCount(t, "comments.body_in=alice+comment,other"); got != 1 {
		t.Errorf("alice's own _in filter returned %d rows, want 1", got)
	}
}

// The same shape one relation direction over: a belongs-to target. The
// many-to-one branch builds a different EXISTS join, and a scope emitted for
// has-many but not for many-to-one leaks through whichever one was missed.
func TestNestedFilterScopesTheManyToOneBranch(t *testing.T) {
	ddl := `
CREATE TABLE comments (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	post_id   TEXT NOT NULL,
	body      TEXT
);
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	title     TEXT
);
`
	commentCfg := makeEntityConfig("comments", "comments", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{entity.BelongsTo("post", "posts", "post_id")}
		},
	)
	postCfg := makeEntityConfig("posts", "posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
	)

	ch, db := setupSecurityTestHandler(t, commentCfg, ddl)
	postEnt := entity.Define(postCfg.Table, postCfg)
	postEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, postEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p-alice", "user_id": "alice", "title": "alice title"},
		{"id": "p-bob", "user_id": "bob", "title": "bob secret title"},
	})
	// Alice owns a comment on each post, so the parent row is hers either way
	// and only the target's scope decides the answer.
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "user_id": "alice", "post_id": "p-alice", "body": "one"},
		{"id": "c2", "user_id": "alice", "post_id": "p-bob", "body": "two"},
	})

	count := func(t *testing.T, query string) int {
		t.Helper()
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/comments?" + query, UserID: "alice"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /comments?%s = %d (body=%s)", query, rr.Code, rr.Body.String())
		}
		var env struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("GET /comments?%s: %v", query, err)
		}
		return len(env.Data)
	}

	if got := count(t, "post.title=alice+title"); got != 1 {
		t.Errorf("alice could not filter her comments by her own post's title: got %d, want 1", got)
	}
	hit := count(t, "post.title=bob+secret+title")
	miss := count(t, "post.title=no+such+title")
	if hit != miss {
		t.Errorf("SECURITY: [oracle] many-to-one branch: guessing bob's title returned %d rows, an absent title %d", hit, miss)
	}
}

// The narrowing must not reach the in-process path. A typed repository is
// server code acting on its own authority — the same carve-out ApplyIncludes
// makes — and silently scoping its queries to whatever identity happened to be
// on the context would break every admin-side report that uses one.
func TestResolveNestedFiltersCarriesNoCallerScope(t *testing.T) {
	target := gateEntity(t, "notes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"notes": target}}
	parent := gateEntity(t, "boards", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
		Relations: []entity.Relation{
			{Type: entity.RelHasMany, Name: "notes", Entity: "notes", ForeignKey: "board_id"},
		},
	})
	got, err := resolveNestedFilters(parent, &reg, []NestedFilter{{Relation: "notes", Field: "body", Value: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].scopes) != 0 {
		t.Errorf("the in-process path gained caller scopes %+v — a typed repo acts on its own authority", got[0].scopes)
	}
	sql, _ := buildExistsSubquery("boards", "id", got[0])
	if strings.Contains(sql, "owner_id") {
		t.Errorf("an in-process subquery was narrowed by owner:\n%s", sql)
	}
}
