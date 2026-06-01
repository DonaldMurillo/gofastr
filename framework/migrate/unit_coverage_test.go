package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// rawEnt builds an entity directly (no Define injection) for migration tests.
func rawEnt(name, table string, fields []schema.Field, rels []entity.Relation, pk string) *entity.Entity {
	e := &entity.Entity{Config: entity.EntityConfig{Name: name, Table: table, Fields: fields, Relations: rels}}
	e.PrimaryKey = pk
	return e
}

func TestRenderMigrationFile_WithAndWithoutDown(t *testing.T) {
	withDown := RenderMigrationFile(3, "add_x", "ALTER TABLE t ADD COLUMN x int;", "ALTER TABLE t DROP COLUMN x;")
	for _, want := range []string{"-- +migrate Version 3", "-- +migrate Name add_x", "-- +migrate Up", "ADD COLUMN x", "-- +migrate Down", "DROP COLUMN x"} {
		if !strings.Contains(withDown, want) {
			t.Errorf("rendered file missing %q:\n%s", want, withDown)
		}
	}
	noDown := RenderMigrationFile(4, "y", "SELECT 1;", "   ")
	if strings.Contains(noDown, "-- +migrate Down") {
		t.Errorf("blank down should omit the Down section:\n%s", noDown)
	}
}

func TestLoadSnapshot_Variants(t *testing.T) {
	dir := t.TempDir()

	// Missing file → empty, no error.
	if s, err := LoadSnapshot(filepath.Join(dir, "nope.json")); err != nil || len(s.Tables) != 0 {
		t.Fatalf("missing: %+v %v", s, err)
	}
	// A directory path → a read error that isn't IsNotExist.
	if _, err := LoadSnapshot(dir); err == nil {
		t.Error("expected a read error for a directory path")
	}
	// Bad JSON → parse error.
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	if _, err := LoadSnapshot(bad); err == nil {
		t.Error("expected a JSON parse error")
	}
	// Valid JSON with null tables → normalized to empty map.
	empty := filepath.Join(dir, "empty.json")
	os.WriteFile(empty, []byte(`{}`), 0o644)
	if s, err := LoadSnapshot(empty); err != nil || s.Tables == nil {
		t.Fatalf("null tables not normalized: %+v %v", s, err)
	}
}

func TestSaveSnapshot_WriteError(t *testing.T) {
	// A path under a non-existent directory → write error.
	if err := SaveSnapshot(filepath.Join(t.TempDir(), "missing-dir", "s.json"), SchemaSnapshot{}); err == nil {
		t.Error("expected a write error for a path in a missing directory")
	}
}

func TestIsFrameworkManagedColumn_AllBranches(t *testing.T) {
	ts := rawEnt("e", "e", nil, nil, "")
	ts.Config.Timestamps = true
	ts.Config.SoftDelete = true
	ts.Config.MultiTenant = true
	for _, c := range []string{"created_at", "updated_at", "deleted_at", "tenant_id"} {
		if !isFrameworkManagedColumn(c, ts) {
			t.Errorf("%q should be managed when all flags on", c)
		}
	}
	off := rawEnt("e", "e", nil, nil, "")
	for _, c := range []string{"created_at", "deleted_at", "tenant_id"} {
		if isFrameworkManagedColumn(c, off) {
			t.Errorf("%q should NOT be managed when flags off", c)
		}
	}
	if isFrameworkManagedColumn("random", ts) {
		t.Error("unknown column should never be managed")
	}
}

func TestSQLType_RawTypeAndDefault(t *testing.T) {
	if got := SQLType(schema.Field{RawType: "NUMERIC(9,2)"}, DialectPostgres); got != "NUMERIC(9,2)" {
		t.Errorf("RawType not honored: %q", got)
	}
	// An out-of-range FieldType falls through to the default TEXT.
	if got := SQLType(schema.Field{Type: schema.FieldType(9999)}, DialectSQLite); got != "TEXT" {
		t.Errorf("unknown type default: %q, want TEXT", got)
	}
}

func TestSQLDefault_FloatAndFallback(t *testing.T) {
	if got := SQLDefault(schema.Field{Default: 3.5}, DialectPostgres); !strings.HasPrefix(got, "3.5") {
		t.Errorf("float default: %q", got)
	}
	// A type the switch doesn't enumerate → quoted %v fallback.
	if got := SQLDefault(schema.Field{Default: []byte("x")}, DialectPostgres); !strings.HasPrefix(got, "'") {
		t.Errorf("fallback default: %q", got)
	}
}

func TestSanitizeIndexExpression_EmptyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on a whitespace-only expression")
		}
	}()
	sanitizeIndexExpression("   ")
}

func TestIndexDDL_InvalidNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on an invalid index name")
		}
	}()
	indexDDL("t", Index{Name: "bad name;", Columns: []string{"a"}})
}

func TestForeignKeyClauses_Branches(t *testing.T) {
	users := rawEnt("users", "users", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")

	// Duplicate FK on the same column → emitted once.
	dupRels := []entity.Relation{
		{Type: entity.RelManyToOne, Name: "a", Entity: "users", ForeignKey: "user_id"},
		{Type: entity.RelManyToOne, Name: "b", Entity: "users", ForeignKey: "user_id"},
	}
	ent := rawEnt("posts", "posts", nil, dupRels, "id")
	fks, err := foreignKeyClauses(ent, map[string]*entity.Entity{"users": users, "posts": ent})
	if err != nil || len(fks) != 1 {
		t.Fatalf("dup FK: got %d clauses, err %v", len(fks), err)
	}

	// Unknown target entity.
	unk := rawEnt("p", "p", nil, []entity.Relation{{Type: entity.RelManyToOne, Name: "r", Entity: "ghost", ForeignKey: "g_id"}}, "id")
	if _, err := foreignKeyClauses(unk, map[string]*entity.Entity{"p": unk}); err == nil {
		t.Error("expected unknown-entity error")
	}

	// Invalid FK column name.
	badFK := rawEnt("p", "p", nil, []entity.Relation{{Type: entity.RelManyToOne, Name: "r", Entity: "users", ForeignKey: "bad col"}}, "id")
	if _, err := foreignKeyClauses(badFK, map[string]*entity.Entity{"p": badFK, "users": users}); err == nil {
		t.Error("expected invalid FK column error")
	}

	// Invalid target table name.
	badTable := rawEnt("bt", "bad table", []schema.Field{{Name: "id"}}, nil, "id")
	refBadTable := rawEnt("p", "p", nil, []entity.Relation{{Type: entity.RelManyToOne, Name: "r", Entity: "bt", ForeignKey: "bt_id"}}, "id")
	if _, err := foreignKeyClauses(refBadTable, map[string]*entity.Entity{"p": refBadTable, "bt": badTable}); err == nil {
		t.Error("expected invalid target table error")
	}

	// Invalid target primary key name.
	badPK := rawEnt("bp", "bp", []schema.Field{{Name: "id"}}, nil, "bad pk")
	refBadPK := rawEnt("p", "p", nil, []entity.Relation{{Type: entity.RelManyToOne, Name: "r", Entity: "bp", ForeignKey: "bp_id"}}, "id")
	if _, err := foreignKeyClauses(refBadPK, map[string]*entity.Entity{"p": refBadPK, "bp": badPK}); err == nil {
		t.Error("expected invalid target PK error")
	}
}

func TestTopoSort_CycleAndNestedUnknown(t *testing.T) {
	// A ↔ B cycle is broken (no error).
	a := rawEnt("a", "a", nil, []entity.Relation{{Type: entity.RelManyToOne, Entity: "b", ForeignKey: "b_id"}}, "id")
	b := rawEnt("b", "b", nil, []entity.Relation{{Type: entity.RelManyToOne, Entity: "a", ForeignKey: "a_id"}}, "id")
	if _, err := topoSortEntities(map[string]*entity.Entity{"a": a, "b": b}); err != nil {
		t.Fatalf("cycle should be tolerated: %v", err)
	}

	// A → B → unknown: the nested error propagates.
	a2 := rawEnt("a", "a", nil, []entity.Relation{{Type: entity.RelManyToOne, Entity: "b", ForeignKey: "b_id"}}, "id")
	b2 := rawEnt("b", "b", nil, []entity.Relation{{Type: entity.RelManyToOne, Entity: "ghost", ForeignKey: "g_id"}}, "id")
	if _, err := topoSortEntities(map[string]*entity.Entity{"a": a2, "b": b2}); err == nil {
		t.Error("expected nested unknown-entity error")
	}
}

func TestBuildCreateTableSQL_NoFieldsUniqueAndFKError(t *testing.T) {
	if _, err := buildCreateTableSQL(rawEnt("e", "e", nil, nil, ""), nil, DialectSQLite); err == nil {
		t.Error("expected error for an entity with no fields")
	}
	ddl, err := buildCreateTableSQL(rawEnt("e", "e", []schema.Field{{Name: "email", Type: schema.String, Unique: true}}, nil, ""), nil, DialectSQLite)
	if err != nil || !strings.Contains(ddl, "UNIQUE") {
		t.Fatalf("unique column DDL: %q err %v", ddl, err)
	}
	// foreignKeyClauses error propagates (valid target, invalid FK column name).
	users := rawEnt("users", "users", []schema.Field{{Name: "id"}}, nil, "id")
	badFK := rawEnt("p", "p", []schema.Field{{Name: "id"}}, []entity.Relation{{Type: entity.RelManyToOne, Entity: "users", ForeignKey: "bad col"}}, "id")
	if _, err := buildCreateTableSQL(badFK, map[string]*entity.Entity{"users": users, "p": badFK}, DialectSQLite); err == nil {
		t.Error("expected buildCreateTableSQL FK error")
	}
}

// TestDiffEntityFromLive_Branches drives the no-fields-create, ADD-NOT-NULL,
// managed-column-skip, and empty-down-type branches directly.
func TestDiffEntityFromLive_Branches(t *testing.T) {
	if _, err := diffEntityFromLive(rawEnt("e", "e", nil, nil, ""), nil, DialectSQLite, map[string]string{}); err == nil {
		t.Error("expected no-fields create error")
	}

	reqEnt := rawEnt("e", "e", []schema.Field{
		{Name: "x", Type: schema.String},
		{Name: "req", Type: schema.String, Required: true, AutoGenerate: schema.AutoNone},
	}, nil, "")
	ch, err := diffEntityFromLive(reqEnt, nil, DialectSQLite, map[string]string{"x": "TEXT"})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var sawNotNull bool
	for _, c := range ch {
		if strings.Contains(c.SQL, "ADD COLUMN req") && strings.Contains(c.SQL, "NOT NULL") {
			sawNotNull = true
		}
	}
	if !sawNotNull {
		t.Fatalf("expected ADD COLUMN req NOT NULL, got %+v", ch)
	}

	tsEnt := rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, "")
	tsEnt.Config.Timestamps = true
	ch2, _ := diffEntityFromLive(tsEnt, nil, DialectSQLite, map[string]string{"x": "TEXT", "created_at": "TIMESTAMP"})
	for _, c := range ch2 {
		if strings.Contains(c.SQL, "DROP COLUMN created_at") {
			t.Fatal("managed created_at should not be dropped")
		}
	}

	ch3, _ := diffEntityFromLive(rawEnt("e", "e", []schema.Field{{Name: "x", Type: schema.String}}, nil, ""),
		nil, DialectSQLite, map[string]string{"x": "TEXT", "legacy": ""})
	var sawTextDown bool
	for _, c := range ch3 {
		if strings.Contains(c.SQL, "DROP COLUMN legacy") && strings.Contains(c.Down, "ADD COLUMN legacy TEXT") {
			sawTextDown = true
		}
	}
	if !sawTextDown {
		t.Fatalf("expected empty-type DROP to re-add as TEXT, got %+v", ch3)
	}
}
