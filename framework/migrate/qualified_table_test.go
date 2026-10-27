package migrate_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// otherSchema creates a Postgres schema outside the test's search_path and
// drops it when the test ends.
func otherSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	var cur string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&cur); err != nil {
		t.Fatal(err)
	}
	s := cur + "_x"
	fkExec(t, db, "CREATE SCHEMA "+s)
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + s + " CASCADE") })
	return s
}

func bootPlan(t *testing.T, db *sql.DB, step string, ents ...*entity.Entity) {
	t.Helper()
	if err := migrate.AutoMigratePlanContext(context.Background(), db, migrate.Plan{Registry: fkReg(ents)}); err != nil {
		t.Fatalf("boot %s: %v", step, err)
	}
}

func widgetsIn(table string, key []string, idx []entity.Index, extra ...schema.Field) *entity.Entity {
	return entity.Define("widgets", entity.EntityConfig{
		Table: table,
		Fields: append([]schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
			{Name: "code", Type: schema.String},
		}, extra...),
		PrimaryKey: key,
		Indices:    idx,
	}.WithTimestamps(false))
}

// pgIndexDef returns the definition of index name in schema s, "" when
// there is none.
func pgIndexDef(t *testing.T, db *sql.DB, s, name string) string {
	t.Helper()
	var def string
	err := db.QueryRow(`SELECT indexdef FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, s, name).Scan(&def)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return def
}

// pgKeyCols lists the primary-key columns of a (qualified) table, in key order.
func pgKeyCols(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT a.attname FROM pg_constraint con
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(n, ord)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n
		WHERE con.contype = 'p' AND con.conrelid = $1::regclass ORDER BY k.ord`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Boot read app.widgets's columns by comparing the catalog's bare widgets
// with the qualified name, found none, took the table for absent, and
// skipped every column added later.
func TestPGQualifiedTableAddsColumn(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	bootPlan(t, db, "1", widgetsIn(s+".widgets", []string{"id"}, nil))
	bootPlan(t, db, "2", widgetsIn(s+".widgets", []string{"id"}, nil,
		schema.Field{Name: "note", Type: schema.String}, schema.Field{Name: "sku", Type: schema.String, Unique: true}))
	fkExec(t, db, `INSERT INTO `+s+`.widgets (id, title, note, sku) VALUES ('w', 't', 'n', 'k')`)
	if _, err := db.Exec(`INSERT INTO ` + s + `.widgets (id, sku) VALUES ('v', 'k')`); err == nil {
		t.Fatal("a repeated sku went in: the added unique index is missing")
	}
	if pgIndexDef(t, db, s, "widgets_sku_key") == "" {
		t.Fatal("no widgets_sku_key in the table's schema")
	}
}

// The same probe on SQLite rendered PRAGMA table_info(main.widgets),
// which SQLite refuses.
// An index on it rendered CREATE INDEX ... ON main.widgets, which SQLite
// refuses: its schema goes on the index name.
func TestSQLiteMainQualifiedTable(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	idx := []entity.Index{{Columns: []string{"title"}}}
	note := schema.Field{Name: "note", Type: schema.String}
	bootPlan(t, db, "1", widgetsIn("main.widgets", []string{"id"}, idx))
	bootPlan(t, db, "2", widgetsIn("main.widgets", []string{"id"}, idx, note))
	fkExec(t, db, `INSERT INTO widgets (id, title, note) VALUES ('w', 't', 'n')`)
	if got := indexNames(t, db, migrate.DialectSQLite, "widgets"); !slices.Equal(got, []string{"idx_widgets_title"}) {
		t.Fatalf("indexes = %v, want [idx_widgets_title]", got)
	}
	changes, err := migrate.DiffSchema(context.Background(), db, fkReg{widgetsIn("main.widgets", []string{"id"}, idx, note)})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		t.Errorf("drift on main.widgets: %s", c.Summary)
	}
}

// A legacy quoted "Title" column is not the title the framework writes
// (its DDL is unquoted, so it folds): matching it without case skipped
// the ADD COLUMN, and every read of title failed.
func TestPGQuotedLegacyColumnNotOurs(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	fkExec(t, db, `CREATE TABLE lw (id TEXT PRIMARY KEY, "Title" TEXT)`)
	ent := entity.Define("lw", entity.EntityConfig{
		Table:      "lw",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "Title", Type: schema.String}},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
	bootPlan(t, db, "1", ent)
	fkExec(t, db, `INSERT INTO lw (id, Title) VALUES ('a', 'x')`)
	var legacy sql.NullString
	if err := db.QueryRow(`SELECT "Title" FROM lw WHERE id = 'a'`).Scan(&legacy); err != nil || legacy.Valid {
		t.Fatalf(`the legacy "Title" column took the write: %v %v`, legacy, err)
	}
}

// The default index name was idx_<table>_<cols>, so app.widgets named
// its index idx_app.widgets_title, which Postgres reads as schema-qualified
// and refuses.
func TestPGQualifiedTableDefaultIndex(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	bootPlan(t, db, "1", widgetsIn(s+".widgets", []string{"id"}, []entity.Index{{Columns: []string{"title"}}}))
	if pgIndexDef(t, db, s, "idx_widgets_title") == "" {
		t.Fatal("no idx_widgets_title in the table's schema")
	}
}

// Replacing an explicit index on app.widgets dropped an unqualified ix,
// which names nothing when app is off the search_path, so the CREATE IF
// NOT EXISTS kept the old definition.
func TestPGQualifiedIndexReplaced(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	note := schema.Field{Name: "note", Type: schema.String}
	migs := genChain(t, db, migrate.DialectPostgres,
		migrate.Plan{Registry: fkReg{widgetsIn(s+".widgets", []string{"id"}, []entity.Index{{Name: "ix", Columns: []string{"title"}}}, note)}},
		migrate.Plan{Registry: fkReg{widgetsIn(s+".widgets", []string{"id"}, []entity.Index{{Name: "ix", Columns: []string{"title", "note"}}, {Columns: []string{"code"}}}, note)}},
	)
	if def := pgIndexDef(t, db, s, "ix"); !strings.Contains(def, "note") {
		t.Fatalf("ix after the replace = %q, want it on (title, note)", def)
	}
	if pgIndexDef(t, db, s, "idx_widgets_code") == "" {
		t.Fatal("no idx_widgets_code in the table's schema")
	}
	genDown(t, db, migrate.DialectPostgres, migs)
	if def := pgIndexDef(t, db, s, "ix"); def == "" || strings.Contains(def, "note") {
		t.Fatalf("ix after rollback = %q, want it on (title)", def)
	}
	if pgIndexDef(t, db, s, "idx_widgets_code") != "" {
		t.Fatal("idx_widgets_code survived the rollback")
	}
}

// A key change on app.widgets dropped the constraint "app.widgets_pkey";
// Postgres named it widgets_pkey.
func TestPGQualifiedKeyChange(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		s := otherSchema(t, db)
		migs := genChain(t, db, migrate.DialectPostgres,
			migrate.Plan{Registry: fkReg{widgetsIn(s+".widgets", []string{"id"}, nil)}},
			migrate.Plan{Registry: fkReg{widgetsIn(s+".widgets", []string{"code"}, nil)}},
		)
		if got := pgKeyCols(t, db, s+".widgets"); !slices.Equal(got, []string{"code"}) {
			t.Fatalf("key after the change = %v, want [code]", got)
		}
		genDown(t, db, migrate.DialectPostgres, migs)
		if got := pgKeyCols(t, db, s+".widgets"); !slices.Equal(got, []string{"id"}) {
			t.Fatalf("key after rollback = %v, want [id]", got)
		}
	})
	t.Run("boot", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		s := otherSchema(t, db)
		bootPlan(t, db, "1", widgetsIn(s+".widgets", []string{"id"}, nil))
		bootPlan(t, db, "2", widgetsIn(s+".widgets", []string{"code"}, nil))
		if got := pgKeyCols(t, db, s+".widgets"); !slices.Equal(got, []string{"code"}) {
			t.Fatalf("key after boot = %v, want [code]", got)
		}
	})
}

func fkParents(table string) *entity.Entity {
	return entity.Define("parents", entity.EntityConfig{
		Table:      table,
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

func fkKids(table string) *entity.Entity {
	return entity.Define("kids", entity.EntityConfig{
		Table:      table,
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "parent_id", Type: schema.String}},
		PrimaryKey: []string{"id"},
		Relations:  []entity.Relation{entity.BelongsTo("parent", "parents", "parent_id")},
	}.WithTimestamps(false))
}

// The live foreign-key reader dropped the referenced table's schema, so a
// key onto other.parents read as one onto the declared parents and was
// never replaced, and a key correctly onto a declared app.parents read as
// drift on every diff.
func TestPGForeignKeyComparesSchema(t *testing.T) {
	ctx := context.Background()
	t.Run("live key onto another schema", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		s := otherSchema(t, db)
		reg := fkReg{fkParents("parents"), fkKids("kids")}
		bootPlan(t, db, "1", reg...)
		fkExec(t, db,
			`CREATE TABLE `+s+`.parents (id TEXT PRIMARY KEY)`,
			`ALTER TABLE kids DROP CONSTRAINT IF EXISTS kids_parent_id_fkey`,
			`ALTER TABLE kids ADD CONSTRAINT kids_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES `+s+`.parents (id)`,
		)
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(changes, func(c migrate.SchemaChange) bool { return strings.Contains(c.Summary, "foreign key") }) {
			t.Fatalf("DiffSchema = %+v, want the key onto %s.parents replaced", changes, s)
		}
		fkApply(t, db, changes)
		fkExec(t, db, `INSERT INTO parents (id) VALUES ('p')`, `INSERT INTO kids (id, parent_id) VALUES ('k', 'p')`)
	})
	t.Run("live key onto the search path", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		s := otherSchema(t, db)
		reg := fkReg{fkParents(s + ".parents"), fkKids("kids")}
		bootPlan(t, db, "1", reg...)
		fkExec(t, db,
			`CREATE TABLE parents (id TEXT PRIMARY KEY)`,
			`ALTER TABLE kids DROP CONSTRAINT IF EXISTS kids_parent_id_fkey`,
			`ALTER TABLE kids ADD CONSTRAINT kids_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES parents (id)`,
		)
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(changes, func(c migrate.SchemaChange) bool { return strings.Contains(c.Summary, "foreign key") }) {
			t.Fatalf("DiffSchema = %+v, want the key onto the search-path parents replaced", changes)
		}
	})
	t.Run("declared key qualified with the search path", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		var cur string
		if err := db.QueryRow(`SELECT current_schema()`).Scan(&cur); err != nil {
			t.Fatal(err)
		}
		reg := fkReg{fkParents(cur + ".parents"), fkKids("kids")}
		bootPlan(t, db, "1", reg...)
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("drift: %s", c.Summary)
		}
	})
	t.Run("qualified child", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		s := otherSchema(t, db)
		reg := fkReg{fkParents("parents"), fkKids(s + ".kids")}
		bootPlan(t, db, "1", reg...)
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("drift: %s", c.Summary)
		}
	})
	t.Run("declared key onto another schema", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		s := otherSchema(t, db)
		reg := fkReg{fkParents(s + ".parents"), fkKids("kids")}
		bootPlan(t, db, "1", reg...)
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("drift: %s", c.Summary)
		}
	})
}

// A key change on a parent detaches the keys onto it, and refuses one no
// relation declares. The declared key of a qualified child app.child was
// listed as app.child:..., the guard compared child:..., and the boot
// failed on the key it should have kept.
func TestPGKeyChangeQualifiedChild(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	child := entity.Define("rkc", entity.EntityConfig{
		Table:     s + ".rkc",
		Fields:    []schema.Field{{Name: "x", Type: schema.String}, {Name: "y", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("rkp", "rkp", "x", "y")},
	}.WithTimestamps(false))
	bootPlan(t, db, "1", rkParent("a", "b"), child)
	bootPlan(t, db, "2", rkParent("b", "a"), child)
	if _, err := db.Exec(`INSERT INTO ` + s + `.rkc (x, y) VALUES ('9', '9')`); err == nil {
		t.Fatal("the child accepted (9, 9), which names no parent: its foreign key is gone")
	}
}

// SQLite names are case-insensitive, but TableExistsBulk compared
// sqlite_master.name exactly, so Tasks read as absent over an existing
// tasks.
func TestSQLiteTableExistsIgnoresCase(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	fkExec(t, db, `CREATE TABLE tasks (id TEXT PRIMARY KEY)`)
	got, err := migrate.TableExistsBulk(context.Background(), db, []string{"Tasks", "main.tasks", "nope"}, migrate.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if !got["Tasks"] || !got["main.tasks"] || got["nope"] {
		t.Fatalf("TableExistsBulk = %v, want Tasks and main.tasks only", got)
	}
	// An attached schema keeps its own catalog.
	fkExec(t, db, `ATTACH DATABASE ':memory:' AS aux`, `CREATE TABLE aux.gadgets (id TEXT)`)
	got, err = migrate.TableExistsBulk(context.Background(), db, []string{"aux.Gadgets", "gadgets"}, migrate.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if !got["aux.Gadgets"] || got["gadgets"] {
		t.Fatalf("TableExistsBulk = %v, want aux.Gadgets only", got)
	}
}

// The Postgres twin compared the qualified name with the bare tablename.
func TestPGTableExistsQualified(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	fkExec(t, db, `CREATE TABLE `+s+`.tasks (id TEXT PRIMARY KEY)`, `CREATE TABLE here (id TEXT)`)
	got, err := migrate.TableExistsBulk(context.Background(), db, []string{s + ".Tasks", "Here", "tasks"}, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if !got[s+".Tasks"] || !got["Here"] || got["tasks"] {
		t.Fatalf("TableExistsBulk = %v, want %s.Tasks and Here only", got, s)
	}
}

// The generator parsed an index's name out of its CREATE INDEX, and a
// SQLite index on a qualified table is named main.ix there: the parse took
// main for the name, so a changed index was never dropped.
func TestSQLiteQualifiedIndexReplaced(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	migs := genChain(t, db, migrate.DialectSQLite,
		migrate.Plan{Registry: fkReg{widgetsIn("main.widgets", []string{"id"}, []entity.Index{{Name: "ix", Columns: []string{"title"}}})}},
		migrate.Plan{Registry: fkReg{widgetsIn("main.widgets", []string{"id"}, []entity.Index{{Name: "ix", Columns: []string{"title", "code"}}})}},
	)
	ixSQL := func() string {
		var ddl string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'ix'`).Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		return ddl
	}
	if ddl := ixSQL(); !strings.Contains(ddl, "code") {
		t.Fatalf("ix after the replace = %q, want it on (title, code)", ddl)
	}
	genDown(t, db, migrate.DialectSQLite, migs)
	if ddl := ixSQL(); strings.Contains(ddl, "code") {
		t.Fatalf("ix after rollback = %q, want it on (title)", ddl)
	}
}

// A many_to_many pivot in another schema named its index
// idx_app.pivot_tag_id, which Postgres refuses, and dropped it unqualified.
func TestPGQualifiedPivotIndex(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	tags := entity.Define("pgt", entity.EntityConfig{
		Table:      "pgt",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
	post := func(pivot bool) *entity.Entity {
		cfg := entity.EntityConfig{
			Table:      "pgp",
			Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
			PrimaryKey: []string{"id"},
		}.WithTimestamps(false)
		if pivot {
			cfg.Relations = []entity.Relation{entity.ManyToMany("tags", "pgt", s+".pgp_tags", "post_id", "tag_id")}
		}
		return entity.Define("pgp", cfg)
	}
	migs := genChain(t, db, migrate.DialectPostgres,
		migrate.Plan{Registry: fkReg{tags, post(false)}},
		migrate.Plan{Registry: fkReg{tags, post(true)}},
	)
	if pgIndexDef(t, db, s, "idx_pgp_tags_tag_id") == "" {
		t.Fatal("no idx_pgp_tags_tag_id in the pivot's schema")
	}
	genDown(t, db, migrate.DialectPostgres, migs)
	if pgIndexDef(t, db, s, "idx_pgp_tags_tag_id") != "" {
		t.Fatal("the pivot index survived the rollback")
	}
}

// sqliteKeyCols lists the primary-key columns of table in schema, in key
// order.
func sqliteKeyCols(t *testing.T, db *sql.DB, schemaName, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?, ?) WHERE pk > 0 ORDER BY pk`, table, schemaName)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A SQLite key change rebuilds the table: create a replacement, copy,
// drop, rename it back. On a qualified table the rename read
// RENAME TO main.widgets, which SQLite refuses (RENAME TO takes a bare
// name), and in an attached schema the foreign keys it renders named
// their parent with its schema, which a SQLite REFERENCES refuses too.
func TestSQLiteRebuildQualifiedTable(t *testing.T) {
	ctx := context.Background()
	idx := []entity.Index{{Columns: []string{"title"}}}
	for _, schemaName := range []string{"main", "aux"} {
		t.Run(schemaName+" diff", func(t *testing.T) {
			db := testdb.Open(t, migrate.DialectSQLite)
			if schemaName != "main" {
				fkExec(t, db, `ATTACH DATABASE ':memory:' AS `+schemaName)
			}
			table := schemaName + ".widgets"
			bootPlan(t, db, "1", widgetsIn(table, []string{"id"}, idx))
			fkExec(t, db, `INSERT INTO `+table+` (id, title, code) VALUES ('w', 't', 'c')`)
			reg := fkReg{widgetsIn(table, []string{"code"}, idx)}
			changes, err := migrate.DiffSchema(ctx, db, reg)
			if err != nil {
				t.Fatal(err)
			}
			fkApply(t, db, changes)
			if got := sqliteKeyCols(t, db, schemaName, "widgets"); !slices.Equal(got, []string{"code"}) {
				t.Fatalf("key after the rebuild = %v, want [code]", got)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM `+table); n != 1 {
				t.Fatalf("%s holds %d rows after the rebuild, want 1", table, n)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM `+schemaName+`.sqlite_master WHERE type = 'index' AND name = 'idx_widgets_title'`); n != 1 {
				t.Fatal("the index did not come back in the table's schema")
			}
			again, err := migrate.DiffSchema(ctx, db, reg)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range again {
				t.Errorf("drift after the rebuild: %s", c.Summary)
			}
		})
	}
	t.Run("aux foreign key", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectSQLite)
		fkExec(t, db, `ATTACH DATABASE ':memory:' AS aux`)
		kids := func(key string) *entity.Entity {
			return entity.Define("kids", entity.EntityConfig{
				Table: "aux.kids",
				Fields: []schema.Field{
					{Name: "id", Type: schema.String, Required: true},
					{Name: "parent_id", Type: schema.String, Required: true},
				},
				PrimaryKey: []string{key},
				Relations:  []entity.Relation{entity.BelongsTo("parent", "parents", "parent_id")},
			}.WithTimestamps(false))
		}
		bootPlan(t, db, "1", fkParents("aux.parents"), kids("id"))
		fkExec(t, db, `INSERT INTO aux.parents (id) VALUES ('p')`, `INSERT INTO aux.kids (id, parent_id) VALUES ('k', 'p')`)
		changes, err := migrate.DiffSchema(ctx, db, fkReg{fkParents("aux.parents"), kids("parent_id")})
		if err != nil {
			t.Fatal(err)
		}
		fkApply(t, db, changes)
		if n := countRows(t, db, `SELECT COUNT(*) FROM pragma_foreign_key_list('kids', 'aux') WHERE "table" = 'parents'`); n != 1 {
			t.Fatalf("aux.kids holds %d key(s) onto parents after the rebuild, want 1", n)
		}
		if _, err := db.Exec(`INSERT INTO aux.kids (id, parent_id) VALUES ('k2', 'nobody')`); err == nil {
			t.Fatal("aux.kids took a row naming no parent")
		}
	})
	t.Run("main generate", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectSQLite)
		genChain(t, db, migrate.DialectSQLite,
			migrate.Plan{Registry: fkReg{widgetsIn("main.widgets", []string{"id"}, idx)}},
			migrate.Plan{Registry: fkReg{widgetsIn("main.widgets", []string{"code"}, idx)}},
		)
		if got := sqliteKeyCols(t, db, "main", "widgets"); !slices.Equal(got, []string{"code"}) {
			t.Fatalf("key after the rebuild = %v, want [code]", got)
		}
	})
}

// A many-to-many pivot in an attached SQLite schema named its parents with
// their schema in its REFERENCES clauses, which SQLite refuses: a key's
// parent is in its child's schema.
func TestSQLiteQualifiedPivot(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	fkExec(t, db, `ATTACH DATABASE ':memory:' AS aux`)
	tags := entity.Define("sqt", entity.EntityConfig{
		Table:      "aux.sqt",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
	posts := entity.Define("sqp", entity.EntityConfig{
		Table:      "aux.sqp",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
		PrimaryKey: []string{"id"},
		Relations:  []entity.Relation{entity.ManyToMany("tags", "sqt", "aux.sqp_tags", "post_id", "tag_id")},
	}.WithTimestamps(false))
	bootPlan(t, db, "1", tags, posts)
	fkExec(t, db,
		`INSERT INTO aux.sqt (id) VALUES ('t')`,
		`INSERT INTO aux.sqp (id) VALUES ('p')`,
		`INSERT INTO aux.sqp_tags (post_id, tag_id) VALUES ('p', 't')`,
	)
	if _, err := db.Exec(`INSERT INTO aux.sqp_tags (post_id, tag_id) VALUES ('p', 'nobody')`); err == nil {
		t.Fatal("the pivot took a row naming no tag")
	}
	fkExec(t, db, `DELETE FROM aux.sqp WHERE id = 'p'`)
	if n := countRows(t, db, `SELECT COUNT(*) FROM aux.sqp_tags`); n != 0 {
		t.Fatalf("the pivot holds %d row(s) after its post was deleted, want 0", n)
	}
}

func countRows(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// The generator orders dropped tables children first, reading each
// recorded CREATE TABLE for its REFERENCES. A qualified reference read as
// one onto the schema alone, so s.ap (sorted first) dropped before s.zc,
// whose key still depended on it.
func TestPGQualifiedDropOrder(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	s := otherSchema(t, db)
	child := entity.Define("zc", entity.EntityConfig{
		Table:      s + ".zc",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "ap_id", Type: schema.String}},
		PrimaryKey: []string{"id"},
		Relations:  []entity.Relation{entity.BelongsTo("ap", "parents", "ap_id")},
	}.WithTimestamps(false))
	migs := genChain(t, db, migrate.DialectPostgres,
		migrate.Plan{Registry: fkReg{fkParents(s + ".ap"), child}},
		migrate.Plan{Registry: fkReg{}},
	)
	genDown(t, db, migrate.DialectPostgres, migs)
	fkExec(t, db, `INSERT INTO `+s+`.ap (id) VALUES ('p')`, `INSERT INTO `+s+`.zc (id, ap_id) VALUES ('c', 'p')`)
}
