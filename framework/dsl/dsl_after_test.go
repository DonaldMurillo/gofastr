package dsl

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// dslRegistry is a minimal entity.Registry for BuildDSLQuery tests.
type dslRegistry struct{ ents map[string]*entity.Entity }

func (r dslRegistry) All() map[string]*entity.Entity { return r.ents }
func (r dslRegistry) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r.ents))
	for _, e := range r.ents {
		out = append(out, e)
	}
	return out
}
func (r dslRegistry) Get(name string) (*entity.Entity, error) {
	if e, ok := r.ents[name]; ok {
		return e, nil
	}
	return nil, errNoSuchEntity
}

var errNoSuchEntity = &dslTestErr{"no such entity"}

type dslTestErr struct{ s string }

func (e *dslTestErr) Error() string { return e.s }

func postsRegistry(t *testing.T, mut func(*entity.EntityConfig)) dslRegistry {
	t.Helper()
	cfg := entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "status", Type: schema.String},
			{Name: "seq", Type: schema.Int},
		},
	}
	if mut != nil {
		mut(&cfg)
	}
	ent := entity.Define("posts", cfg)
	return dslRegistry{ents: map[string]*entity.Entity{"posts": ent}}
}

// TestBuildDSLAfterWiresCursor pins that after(cursor) produces a keyset
// predicate in the built query, not a silent page-1 result.
func TestBuildDSLAfterWiresCursor(t *testing.T) {
	reg := postsRegistry(t, nil)
	qb, err := BuildDSLQuery(reg, `posts.where(status="published").after("p100").limit(5)`)
	if err != nil {
		t.Fatalf("BuildDSLQuery: %v", err)
	}
	sql, args := qb.Build()
	// Default cursor column is the primary key (id).
	if !strings.Contains(sql, "id >") {
		t.Fatalf("after() did not wire a keyset predicate; sql=%s", sql)
	}
	found := false
	for _, a := range args {
		if a == "p100" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cursor value p100 missing from args=%#v (sql=%s)", args, sql)
	}
}

// TestBuildDSLAfterUsesCursorField pins that after() keysets on the entity's
// configured CursorField, not always the primary key.
func TestBuildDSLAfterUsesCursorField(t *testing.T) {
	reg := postsRegistry(t, func(c *entity.EntityConfig) { c.CursorField = "seq" })
	qb, err := BuildDSLQuery(reg, `posts.after("42")`)
	if err != nil {
		t.Fatalf("BuildDSLQuery: %v", err)
	}
	sql, _ := qb.Build()
	if !strings.Contains(sql, "seq >") {
		t.Fatalf("after() ignored CursorField; sql=%s", sql)
	}
}

// TestBuildDSLAfterCompositeRejected pins that after() on a composite-cursor
// entity errors instead of silently ignoring the cursor.
func TestBuildDSLAfterCompositeRejected(t *testing.T) {
	reg := postsRegistry(t, func(c *entity.EntityConfig) { c.CursorFields = []string{"seq", "id"} })
	if _, err := BuildDSLQuery(reg, `posts.after("x")`); err == nil {
		t.Fatal("after() on composite cursor should error")
	}
}
