package crud

import (
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

func TestFiltered_InvalidRelEntity(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "bad ent!", ForeignKey: "fk"}, Target: guardTarget("bad ent!")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "target table") {
		t.Fatalf("invalid target table err = %v", err)
	}
}

func TestFiltered_InvalidParentTable(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "bad table!", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "parent table") {
		t.Fatalf("invalid parent table err = %v", err)
	}
}

func TestFiltered_InvalidParentPK(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "bad pk!", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "parent PK") {
		t.Fatalf("invalid parent PK err = %v", err)
	}
}

func TestFiltered_InvalidFKHasMany(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "bad fk!"}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "FK") {
		t.Fatalf("invalid FK hasMany err = %v", err)
	}
}

func TestFiltered_InvalidFKBelongsTo(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelManyToOne, Name: "r", Entity: "ent", ForeignKey: "bad fk!"}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "FK") {
		t.Fatalf("invalid FK belongsTo err = %v", err)
	}
}

func TestFiltered_UnknownRelType(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	// Cast a bogus RelationType so the switch falls through to `return nil`.
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelationType(99), Name: "r", Entity: "ent", ForeignKey: "fk"}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err != nil {
		t.Fatalf("unknown rel type should fall through to nil, got %v", err)
	}
}

func TestFiltered_InvalidFilterField(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{
		Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"},
		Target:   guardTarget("ent"),
		Filters:  []filter.ParsedFilter{{Field: "bad field!", Op: filter.OpEq, Value: "x"}},
	}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "filter field") {
		t.Fatalf("invalid filter field err = %v", err)
	}
}

func TestFiltered_M2MInvalidThrough(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{
		Type: entity.RelManyToMany, Name: "r", Entity: "ent",
		Through: "bad through!", LocalKey: "lk", ForeignKeyTarget: "fkt",
	}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "through") {
		t.Fatalf("invalid through err = %v", err)
	}
}

func TestFiltered_M2MInvalidLocalKey(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{
		Type: entity.RelManyToMany, Name: "r", Entity: "ent",
		Through: "pivot", LocalKey: "bad key!", ForeignKeyTarget: "fkt",
	}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "local key") {
		t.Fatalf("invalid local key err = %v", err)
	}
}

func TestFiltered_M2MInvalidFKTarget(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{
		Type: entity.RelManyToMany, Name: "r", Entity: "ent",
		Through: "pivot", LocalKey: "lk", ForeignKeyTarget: "bad!",
	}, Target: guardTarget("ent")}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"), newIncludeBudget())
	if err == nil || !strings.Contains(err.Error(), "FK target") {
		t.Fatalf("invalid FK target err = %v", err)
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
	err := loadIncludeNode(context.Background(), db, "src", "id", node, []string{"nope"}, newResult("nope"), newIncludeBudget())
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
	err := loadIncludeNode(context.Background(), db, "src", "id", node, []string{"s1"}, result, newIncludeBudget())
	if err != nil {
		t.Fatalf("NULL fk should not error, got: %v", err)
	}
	if _, present := result["s1"]["author"]; present {
		t.Errorf("NULL fk should leave author absent; got %v", result["s1"]["author"])
	}
}
