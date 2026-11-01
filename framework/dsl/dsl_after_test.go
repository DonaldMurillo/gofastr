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
			// Required: a keyset column must never hold NULL (Define).
			{Name: "seq", Type: schema.Int, Required: true},
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
	reg := postsRegistry(t, func(c *entity.EntityConfig) {
		if c.Pagination == nil {
			c.Pagination = &entity.PaginationConfig{}
		}
		c.Pagination.CursorField = "seq"
		// after() keysets on the one column, so it must be unique.
		c.Fields[1].Unique = true
	})
	qb, err := BuildDSLQuery(reg, `posts.after("42")`)
	if err != nil {
		t.Fatalf("BuildDSLQuery: %v", err)
	}
	sql, args := qb.Build()
	if !strings.Contains(sql, "seq >") {
		t.Fatalf("after() ignored CursorField; sql=%s", sql)
	}
	// The bare keyset value binds as the column's type.
	if len(args) != 1 || args[0] != int64(42) {
		t.Fatalf("after(\"42\") on an Int cursor bound %#v, want int64(42)", args)
	}
	// And a value not of that type is refused, not bound as text.
	if _, err := BuildDSLQuery(reg, `posts.after("abc")`); err == nil || !strings.Contains(err.Error(), "not a valid int") {
		t.Fatalf("after(\"abc\") on an Int cursor: err = %v, want a not-a-valid-int refusal", err)
	}
}

// A where() value that is not of its column's type is an error naming the
// field, the same refusal ?field= answers, not a silent text bind that
// Postgres refuses at query time and SQLite compares as text.
func TestBuildDSLValueWrongTypeErrs(t *testing.T) {
	reg := postsRegistry(t, nil)
	for _, q := range []string{
		`posts.where(seq="abc")`,
		`posts.where(seq>"1.5")`,
		`posts.where(seq in [1, x])`,
		`posts.where(seq contains "4")`,
	} {
		if _, err := BuildDSLQuery(reg, q); err == nil || !strings.Contains(err.Error(), `"seq"`) {
			t.Errorf("%s: err = %v, want a refusal naming \"seq\"", q, err)
		}
	}
	qb, err := BuildDSLQuery(reg, `posts.where(seq>="7")`)
	if err != nil {
		t.Fatalf("well-typed where: %v", err)
	}
	if _, args := qb.Build(); len(args) != 1 || args[0] != int64(7) {
		t.Fatalf("seq>=7 bound %#v, want int64(7)", args)
	}
}

// TestBuildDSLAfterCompositeRejected pins that after() on a composite-cursor
// entity errors instead of silently ignoring the cursor.
func TestBuildDSLAfterCompositeRejected(t *testing.T) {
	reg := postsRegistry(t, func(c *entity.EntityConfig) {
		if c.Pagination == nil {
			c.Pagination = &entity.PaginationConfig{}
		}
		c.Pagination.CursorFields = []string{"seq", "id"}
	})
	if _, err := BuildDSLQuery(reg, `posts.after("x")`); err == nil {
		t.Fatal("after() on composite cursor should error")
	}
}

// after() keysets on CursorField alone, so a non-unique one would skip
// every row tied with the value it resumes after (two posts with one
// created_at). It is refused with the fix named, not run (bug hunt r7
// R7-002); the HTTP cursor API appends the key as a tiebreak.
func TestDSLAfterRefusesNonUniqueCursor(t *testing.T) {
	reg := postsRegistry(t, func(c *entity.EntityConfig) {
		c.Pagination = &entity.PaginationConfig{CursorField: "seq"}
	})
	_, err := BuildDSLQuery(reg, `posts.order(seq).after("42").limit(1)`)
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("after() on a non-unique CursorField: err = %v, want a not-unique refusal", err)
	}
}
