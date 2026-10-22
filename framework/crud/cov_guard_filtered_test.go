package crud

import (
	"cmp"
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// These tests construct IncludeNode / Relation values directly so the
// validation guards inside loadIncludeNode (normally unreachable through
// entity.Define, which never emits invalid identifiers) actually fire.
//
// Every node carries a Target: loadIncludeNode refuses an unresolved one
// up front (that is the guard TestIncludeUnregisteredTargetFails pins), so
// without it none of the identifier guards below would be reached.

// guardTarget builds a throwaway target entity for the given table so a
// hand-built IncludeNode looks like one parseIncludeTree produced.
func guardTarget(table string) *entity.Entity {
	return entity.Define(table, entity.EntityConfig{
		Table:  table,
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	})
}

func newResult(ids ...string) map[string]map[string]any {
	r := map[string]map[string]any{}
	for _, id := range ids {
		r[id] = map[string]any{}
	}
	return r
}

// TestFiltered_IdentifierGuards: each case breaks one identifier
// loadIncludeNode interpolates and expects the guard naming it to refuse
// the load. A bogus RelationType falls through the switch to nil.
func TestFiltered_IdentifierGuards(t *testing.T) {
	hasMany := entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"}
	m2m := func(through, local, target string) entity.Relation {
		return entity.Relation{Type: entity.RelManyToMany, Name: "r", Entity: "ent", Through: through, LocalKey: local, ForeignKeyTarget: target}
	}
	with := func(r entity.Relation, edit func(*entity.Relation)) entity.Relation {
		edit(&r)
		return r
	}
	cases := []struct {
		name      string
		table     string
		parentKey string
		rel       entity.Relation
		filters   []filter.ParsedFilter
		want      string // "" means the load succeeds
	}{
		{name: "target table", rel: with(hasMany, func(r *entity.Relation) { r.Entity = "bad ent!" }), want: "target table"},
		{name: "parent table", table: "bad table!", rel: hasMany, want: "parent table"},
		{name: "parent PK", parentKey: "bad pk!", rel: hasMany, want: "parent PK"},
		{name: "has_many FK", rel: with(hasMany, func(r *entity.Relation) { r.ForeignKey = "bad fk!" }), want: "FK"},
		{name: "belongs_to FK", rel: with(hasMany, func(r *entity.Relation) { r.Type, r.ForeignKey = entity.RelManyToOne, "bad fk!" }), want: "FK"},
		{name: "unknown type", rel: with(hasMany, func(r *entity.Relation) { r.Type = entity.RelationType(99) })},
		{name: "filter field", rel: hasMany, filters: []filter.ParsedFilter{{Field: "bad field!", Op: filter.OpEq, Value: "x"}}, want: "filter field"},
		{name: "m2m through", rel: m2m("bad through!", "lk", "fkt"), want: "through"},
		{name: "m2m local key", rel: m2m("pivot", "bad key!", "fkt"), want: "local key"},
		{name: "m2m FK target", rel: m2m("pivot", "lk", "bad!"), want: "FK target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
			table, pk := cmp.Or(tc.table, "p"), cmp.Or(tc.parentKey, "id")
			node := &IncludeNode{Relation: tc.rel, Target: guardTarget(tc.rel.Entity), Filters: tc.filters}
			err := loadIncludeNode(context.Background(), db, table, []string{pk}, node, []string{"1"}, newResult("1"), newIncludeBudget())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming %q, got %v", tc.want, err)
			}
		})
	}
}

// loadBelongsToFiltered len(fks)==0 early return: no source rows match.
func TestFiltered_BelongsToNoSourceRows(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE src (id TEXT PRIMARY KEY, author_id TEXT)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
	)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"}, Target: guardTarget("users")}
	// ids reference no existing src rows → source query yields nothing → fks empty.
	err := loadIncludeNode(context.Background(), db, "src", []string{"id"}, node, []string{"nope"}, newResult("nope"), newIncludeBudget())
	if err != nil {
		t.Fatalf("no-source-rows belongsTo = %v", err)
	}
}

// A NULL foreign key (optional relation) must NOT error inside
// loadBelongsToFiltered; the parent keeps the relation unset. (issue #66)
func TestFiltered_BelongsToScanNullFK(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE src (id TEXT PRIMARY KEY, author_id TEXT)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
	)
	if _, err := db.Exec(`INSERT INTO src (id, author_id) VALUES ('s1', NULL)`); err != nil {
		t.Fatal(err)
	}
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"}, Target: guardTarget("users")}
	result := newResult("s1")
	err := loadIncludeNode(context.Background(), db, "src", []string{"id"}, node, []string{"s1"}, result, newIncludeBudget())
	if err != nil {
		t.Fatalf("NULL fk should not error, got: %v", err)
	}
	if _, present := result["s1"]["author"]; present {
		t.Errorf("NULL fk should leave author absent; got %v", result["s1"]["author"])
	}
}
