package crud

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// These tests construct IncludeNode / Relation values directly so the
// validation guards inside loadIncludeNode (normally unreachable through
// entity.Define, which never emits invalid identifiers) actually fire.

func newResult(ids ...string) map[string]map[string]any {
	r := map[string]map[string]any{}
	for _, id := range ids {
		r[id] = map[string]any{}
	}
	return r
}

func TestFiltered_InvalidRelEntity(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "bad ent!", ForeignKey: "fk"}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "relation entity") {
		t.Fatalf("invalid rel entity err = %v", err)
	}
}

func TestFiltered_InvalidParentTable(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"}}
	err := loadIncludeNode(context.Background(), db, "bad table!", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "parent table") {
		t.Fatalf("invalid parent table err = %v", err)
	}
}

func TestFiltered_InvalidParentPK(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"}}
	err := loadIncludeNode(context.Background(), db, "p", "bad pk!", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "parent PK") {
		t.Fatalf("invalid parent PK err = %v", err)
	}
}

func TestFiltered_InvalidFKHasMany(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "bad fk!"}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "FK") {
		t.Fatalf("invalid FK hasMany err = %v", err)
	}
}

func TestFiltered_InvalidFKBelongsTo(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelManyToOne, Name: "r", Entity: "ent", ForeignKey: "bad fk!"}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "FK") {
		t.Fatalf("invalid FK belongsTo err = %v", err)
	}
}

func TestFiltered_UnknownRelType(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	// Cast a bogus RelationType so the switch falls through to `return nil`.
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelationType(99), Name: "r", Entity: "ent", ForeignKey: "fk"}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err != nil {
		t.Fatalf("unknown rel type should fall through to nil, got %v", err)
	}
}

func TestFiltered_InvalidFilterField(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{
		Relation: entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "ent", ForeignKey: "fk"},
		Filters:  []filter.ParsedFilter{{Field: "bad field!", Op: filter.OpEq, Value: "x"}},
	}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "filter field") {
		t.Fatalf("invalid filter field err = %v", err)
	}
}

func TestFiltered_M2MInvalidThrough(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{
		Type: entity.RelManyToMany, Name: "r", Entity: "ent",
		Through: "bad through!", LocalKey: "lk", ForeignKeyTarget: "fkt",
	}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "through") {
		t.Fatalf("invalid through err = %v", err)
	}
}

func TestFiltered_M2MInvalidLocalKey(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{
		Type: entity.RelManyToMany, Name: "r", Entity: "ent",
		Through: "pivot", LocalKey: "bad key!", ForeignKeyTarget: "fkt",
	}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
	if err == nil || !strings.Contains(err.Error(), "local key") {
		t.Fatalf("invalid local key err = %v", err)
	}
}

func TestFiltered_M2MInvalidFKTarget(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{Relation: entity.Relation{
		Type: entity.RelManyToMany, Name: "r", Entity: "ent",
		Through: "pivot", LocalKey: "lk", ForeignKeyTarget: "bad!",
	}}
	err := loadIncludeNode(context.Background(), db, "p", "id", node, []string{"1"}, newResult("1"))
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
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"}}
	// ids reference no existing src rows → source query yields nothing → fks empty.
	err := loadIncludeNode(context.Background(), db, "src", "id", node, []string{"nope"}, newResult("nope"))
	if err != nil {
		t.Fatalf("no-source-rows belongsTo = %v", err)
	}
}

// Scanning a NULL foreign key into *string fails inside loadBelongsToFiltered.
func TestFiltered_BelongsToScanNullFK(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE src (id TEXT PRIMARY KEY, author_id TEXT)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
	)
	if _, err := db.Exec(`INSERT INTO src (id, author_id) VALUES ('s1', NULL)`); err != nil {
		t.Fatal(err)
	}
	node := &IncludeNode{Relation: entity.Relation{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"}}
	err := loadIncludeNode(context.Background(), db, "src", "id", node, []string{"s1"}, newResult("s1"))
	if err == nil {
		t.Fatal("scanning NULL fk into *string should error")
	}
}
