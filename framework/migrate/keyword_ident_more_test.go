package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The rest of the DDL the migrator writes, on keyword names: a rename and
// a drop of a keyword column, Unique and NOT NULL set and cleared on one
// (the snapshot's recorded CREATE TABLE read back), a pivot whose table
// and columns are keywords, a key change on a keyword table, and the
// refusals that read the recorded REFERENCES of a keyword table.

func kwTable(name, table string, key []string, fields ...schema.Field) *entity.Entity {
	return entity.Define(name, entity.EntityConfig{Table: table, Fields: fields, PrimaryKey: key}.WithTimestamps(false))
}

func TestKeywordColumnsAlterThroughGenerate(t *testing.T) {
	ent := func(stage int) *entity.Entity {
		fields := []schema.Field{{Name: "id", Type: schema.String, Required: true}}
		switch stage {
		case 1:
			fields = append(fields,
				schema.Field{Name: "from", Type: schema.String},
				schema.Field{Name: "end", Type: schema.String, Default: "x"},
				schema.Field{Name: "desc", Type: schema.String},
				schema.Field{Name: "all", Type: schema.String})
		case 2: // from renamed to to, end made NOT NULL with a new default, desc Unique, all dropped
			fields = append(fields,
				schema.Field{Name: "to", Type: schema.String},
				schema.Field{Name: "end", Type: schema.String, Required: true, Default: "y"},
				schema.Field{Name: "desc", Type: schema.String, Unique: true})
		case 3: // desc's Unique cleared, end optional again
			fields = append(fields,
				schema.Field{Name: "to", Type: schema.String},
				schema.Field{Name: "end", Type: schema.String, Default: "y"},
				schema.Field{Name: "desc", Type: schema.String})
		}
		e := kwTable("kwa", "select", []string{"id"}, fields...)
		e.Config.Indices = []entity.Index{{Columns: []string{"end"}}, {Name: "where", Columns: []string{"id", "end"}}}
		if stage >= 2 {
			e.Config.Renames = map[string]string{"from": "to"}
		}
		return e
	}
	plan := func(stage int) migrate.Plan { return migrate.Plan{Registry: fkReg{ent(stage)}} }
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		genChain(t, db, d, plan(1))
		fkExec(t, db, `INSERT INTO "select" ("id", "from", "end", "desc") VALUES ('r1', 'f', 'e', 'd')`)
		_, _, snap1, err := migrate.GeneratePlan(plan(1), migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		up2, _, snap2, err := migrate.GeneratePlan(plan(2), snap1, d)
		if err != nil {
			t.Fatal(err)
		}
		fkRunStep(t, db, d, 2, up2)
		noUniques := snap2
		noUniques.UniqueColumns = nil
		if up, _, _, err := migrate.GeneratePlan(plan(2), noUniques, d); err != nil || up != "" {
			t.Fatalf("a snapshot recording desc's UNIQUE only in its CREATE TABLE diffed as %q, %v", up, err)
		}
		var to, end string
		if err := db.QueryRow(`SELECT "to", "end" FROM "select" WHERE "id" = 'r1'`).Scan(&to, &end); err != nil || to != "f" || end != "e" {
			t.Fatalf("after the rename: to=%q end=%q err=%v", to, end, err)
		}
		if _, err := db.Exec(`INSERT INTO "select" ("id", "desc") VALUES ('r2', 'd')`); err == nil {
			t.Fatal("desc made Unique takes duplicates")
		}
		if _, err := db.Exec(`INSERT INTO "select" ("id", "end") VALUES ('r3', NULL)`); err == nil {
			t.Fatal("end made Required takes NULL")
		}
		fkExec(t, db, `INSERT INTO "select" ("id") VALUES ('r4')`)
		if err := db.QueryRow(`SELECT "end" FROM "select" WHERE "id" = 'r4'`).Scan(&end); err != nil || end != "y" {
			t.Fatalf("end's new default = %q (%v), want y", end, err)
		}
		up3, _, snap3, err := migrate.GeneratePlan(plan(3), snap2, d)
		if err != nil {
			t.Fatal(err)
		}
		fkRunStep(t, db, d, 3, up3)
		fkExec(t, db, `INSERT INTO "select" ("id", "desc", "end") VALUES ('r5', 'd', NULL)`)
		if up, _, _, err := migrate.GeneratePlan(plan(3), snap3, d); err != nil || up != "" {
			t.Fatalf("a generation with nothing changed = %q, %v", up, err)
		}
		// A snapshot an older build wrote, with a keyword name bare: the
		// quoting alone is no change. (On SQLite the bare spelling ran.)
		old := snap3
		old.Indices = map[string][]string{}
		for table, ddls := range snap3.Indices {
			for _, ddl := range ddls {
				old.Indices[table] = append(old.Indices[table], strings.ReplaceAll(ddl, `"`, ""))
			}
		}
		old.TableDDL = map[string]string{}
		for table, ddl := range snap3.TableDDL {
			old.TableDDL[table] = strings.ReplaceAll(ddl, `"`, "")
		}
		old.UniqueColumns = nil
		if up, _, _, err := migrate.GeneratePlan(plan(3), old, d); err != nil || up != "" {
			t.Fatalf("a snapshot with the keyword names bare diffed as %q, %v", up, err)
		}
	})
}

// fkRunStep applies one generated Up as migration version v, on top of
// whatever genChain applied as version 1.
func fkRunStep(t *testing.T, db *sql.DB, d migrate.Dialect, v uint64, up string) {
	t.Helper()
	if up == "" {
		t.Fatalf("step %d generated nothing", v)
	}
	if _, err := db.Exec(up); err != nil {
		t.Fatalf("step %d:\n%s\n: %v", v, up, err)
	}
}

func TestKeywordPivotAndKeyChange(t *testing.T) {
	reg := func(key string) fkReg {
		parent := kwTable("kwp", "user", []string{key},
			schema.Field{Name: "group", Type: schema.String, Required: true},
			schema.Field{Name: "only", Type: schema.String, Required: true})
		other := entity.Define("kwo", entity.EntityConfig{
			Table:      "table",
			Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
			PrimaryKey: []string{"id"},
			Relations:  []entity.Relation{entity.ManyToMany("users", "kwp", "order", "do", "to")},
		}.WithTimestamps(false))
		return fkReg{parent, other}
	}
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		for range 2 {
			if err := migrate.AutoMigrate(db, reg("group")); err != nil {
				t.Fatalf("boot: %v", err)
			}
		}
		kwDiffEmpty(t, db, reg("group"))
		fkExec(t, db, `INSERT INTO "user" ("group", "only") VALUES ('g', 'o')`, `INSERT INTO "table" ("id") VALUES ('t')`,
			`INSERT INTO "order" ("do", "to") VALUES ('t', 'g')`)
		if _, err := db.Exec(`INSERT INTO "order" ("do", "to") VALUES ('t', 'nope')`); err == nil {
			t.Fatal("the pivot's foreign key onto user(group) does not hold")
		}
		// The key of user moves from group to only: the pivot references
		// it, so the change runs through DiffSchema with its foreign key
		// detached and put back on Postgres; SQLite refuses a rebuild that
		// strands the pivot.
		fkExec(t, db, `DELETE FROM "order"`)
		changes, err := migrate.DiffSchema(context.Background(), db, reg("only"))
		if d == migrate.DialectSQLite {
			return
		}
		if err != nil {
			t.Fatalf("DiffSchema for the key change: %v", err)
		}
		if _, err := migrate.ApplySchemaDiffWithOptions(context.Background(), db, changes, migrate.ApplyOptions{AllowDestructive: true}); err != nil {
			t.Fatalf("apply the key change: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO "user" ("group", "only") VALUES ('g2', 'o')`); err == nil {
			t.Fatal("only is not the key after the change")
		}
	})
}

// GeneratePlan reads the REFERENCES of the snapshot's recorded CREATE
// TABLE to refuse a SQLite rebuild that a foreign key onto the table would
// fail or cascade from; a keyword parent is written quoted there.
func TestKeywordParentRebuildRefused(t *testing.T) {
	for _, onDelete := range []entity.OnDeleteAction{"", entity.OnDeleteCascade} {
		parent := func(tp schema.FieldType) *entity.Entity {
			return kwTable("kwp", "user", []string{"id"},
				schema.Field{Name: "id", Type: schema.String, Required: true},
				schema.Field{Name: "n", Type: tp})
		}
		rel := entity.BelongsTo("owner", "kwp", "to")
		rel.OnDelete = onDelete
		child := entity.Define("kwc", entity.EntityConfig{
			Table:      "kwc",
			Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "to", Type: schema.String}},
			PrimaryKey: []string{"id"},
			Relations:  []entity.Relation{rel},
		}.WithTimestamps(false))
		_, _, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.Int), child}}, migrate.SchemaSnapshot{}, migrate.DialectSQLite)
		if err != nil {
			t.Fatal(err)
		}
		snap.UniqueColumns = nil
		_, _, _, err = migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.String), child}}, snap, migrate.DialectSQLite)
		if err == nil || !strings.Contains(err.Error(), "kwc") {
			t.Fatalf("ON DELETE %q: a rebuild of user that kwc references = %v, want a refusal naming kwc", onDelete, err)
		}
	}
}

// A CASCADE key the snapshot records onto a keyword table counts after its
// relation is removed in the same change: the key is still on the child
// when the file runs.
func TestKeywordParentRebuildRefusedAfterRelationRemoved(t *testing.T) {
	parent := func(tp schema.FieldType) *entity.Entity {
		return kwTable("kwp", "user", []string{"id"},
			schema.Field{Name: "id", Type: schema.String, Required: true},
			schema.Field{Name: "n", Type: tp})
	}
	child := func(rels ...entity.Relation) *entity.Entity {
		return entity.Define("kwc", entity.EntityConfig{
			Table:      "kwc",
			Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "to", Type: schema.String}},
			PrimaryKey: []string{"id"},
			Relations:  rels,
		}.WithTimestamps(false))
	}
	rel := entity.BelongsTo("owner", "kwp", "to")
	rel.OnDelete = entity.OnDeleteCascade
	_, _, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.Int), child(rel)}}, migrate.SchemaSnapshot{}, migrate.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.String), child()}}, snap, migrate.DialectSQLite)
	if err == nil || !strings.Contains(err.Error(), "CASCADE") || !strings.Contains(err.Error(), "kwc") {
		t.Fatalf("a rebuild of user that kwc's recorded CASCADE key references = %v, want the CASCADE refusal naming kwc", err)
	}
}

// Dropping a keyword parent and its child in one generation drops the
// child first: the order reads the recorded REFERENCES "user". The child
// sorts after the parent by name, so only the reference orders them.
func TestKeywordDroppedTablesOrdered(t *testing.T) {
	parent := kwTable("kwp", "user", []string{"id"}, schema.Field{Name: "id", Type: schema.String, Required: true})
	child := entity.Define("kwc", entity.EntityConfig{
		Table:      "zz_child",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "to", Type: schema.String}},
		PrimaryKey: []string{"id"},
		Relations:  []entity.Relation{entity.BelongsTo("owner", "kwp", "to")},
	}.WithTimestamps(false))
	keep := kwTable("keep", "keep", []string{"id"}, schema.Field{Name: "id", Type: schema.String, Required: true})
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		genChain(t, db, d, migrate.Plan{Registry: fkReg{parent, child, keep}}, migrate.Plan{Registry: fkReg{keep}})
		for _, table := range []string{"user", "zz_child"} {
			var n int
			q := `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`
			if d == migrate.DialectPostgres {
				q = `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = $1`
			}
			if err := db.QueryRow(q, table).Scan(&n); err != nil || n != 0 {
				t.Fatalf("%s still exists after the drop (%d, %v)", table, n, err)
			}
		}
	})
}

// Boot changes the key of an empty table; it reads the table for a row
// first, and that read names a keyword table too.
func TestKeywordTableKeyChangeAtBoot(t *testing.T) {
	reg := func(key string) fkReg {
		return fkReg{kwTable("kws", "select", []string{key},
			schema.Field{Name: "id", Type: schema.String, Required: true},
			schema.Field{Name: "group", Type: schema.String, Required: true})}
	}
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if err := migrate.AutoMigrate(db, reg("id")); err != nil {
			t.Fatalf("first boot: %v", err)
		}
		if err := migrate.AutoMigrate(db, reg("group")); err != nil {
			t.Fatalf("boot moving the key to group: %v", err)
		}
		fkExec(t, db, `INSERT INTO "select" ("id", "group") VALUES ('a', 'g')`)
		if _, err := db.Exec(`INSERT INTO "select" ("id", "group") VALUES ('b', 'g')`); err == nil {
			t.Fatal("group is not the key after the boot")
		}
		kwDiffEmpty(t, db, reg("group"))
	})
}
