package migrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A live SQLite rebuild (ApplySchemaDiffWithOptions) drops the table, and
// DROP TABLE takes the table's own triggers with it. Boot and the
// generated file put declared routines back by replaying them; the live
// apply had nothing to replay, so a key change or retype succeeded and
// left the table with no trigger. It now reads the table's triggers
// before the set runs and recreates them after it.
func TestLiveSQLiteRebuildKeepsTriggers(t *testing.T) {
	ctx := context.Background()
	for _, schemaName := range []string{"main", "aux"} {
		t.Run(schemaName, func(t *testing.T) {
			db := testdb.Open(t, migrate.DialectSQLite)
			if schemaName != "main" {
				fkExec(t, db, `ATTACH DATABASE ':memory:' AS `+schemaName)
			}
			table := schemaName + ".widgets"
			bootPlan(t, db, "1", widgetsIn(table, []string{"id"}, nil))
			fkExec(t, db,
				`CREATE TABLE `+schemaName+`.widget_log (id TEXT, op TEXT)`,
				// The second trigger names the table in another case:
				// SQLite resolves it, and sqlite_master keeps the spelling.
				`CREATE TRIGGER `+schemaName+`.widgets_ins AFTER INSERT ON widgets BEGIN INSERT INTO widget_log VALUES (NEW.id, 'ins'); END`,
				`CREATE TRIGGER `+schemaName+`.widgets_del AFTER DELETE ON Widgets BEGIN INSERT INTO widget_log VALUES (OLD.id, 'del'); END`,
				// A trigger on another table stays where it is.
				`CREATE TRIGGER `+schemaName+`.log_touch AFTER INSERT ON widget_log BEGIN SELECT 1; END`,
				`INSERT INTO `+table+` (id, title, code) VALUES ('a', 't', 'ca')`,
			)
			changes, err := migrate.DiffSchema(ctx, db, fkReg{widgetsIn(table, []string{"code"}, nil)})
			if err != nil {
				t.Fatal(err)
			}
			fkApply(t, db, changes)
			if got := sqliteKeyCols(t, db, schemaName, "widgets"); len(got) != 1 || got[0] != "code" {
				t.Fatalf("key after the apply = %v, want [code]: the rebuild did not run", got)
			}
			for _, name := range []string{"widgets_ins", "widgets_del", "log_touch"} {
				if n := countRows(t, db, `SELECT COUNT(*) FROM `+schemaName+`.sqlite_master WHERE type = 'trigger' AND name = '`+name+`'`); n != 1 {
					t.Errorf("trigger %s: %d after the rebuild, want 1", name, n)
				}
			}
			// The copy into the replacement table fired nothing.
			if n := countRows(t, db, `SELECT COUNT(*) FROM `+schemaName+`.widget_log`); n != 1 {
				t.Fatalf("widget_log holds %d rows after the rebuild, want 1 (the seed insert)", n)
			}
			fkExec(t, db,
				`INSERT INTO `+table+` (id, title, code) VALUES ('b', 't', 'cb')`,
				`DELETE FROM `+table+` WHERE id = 'a'`,
			)
			if n := countRows(t, db, `SELECT COUNT(*) FROM `+schemaName+`.widget_log WHERE (id, op) IN (VALUES ('b', 'ins'), ('a', 'del'))`); n != 2 {
				t.Fatalf("the triggers fired %d of 2 times after the rebuild", n)
			}
		})
	}
}

// The set renames a column before it rebuilds the table, and RENAME
// COLUMN rewrites the trigger body that names it. The rebuild must put
// back the trigger as the rename left it: one read before the whole set
// ran would recreate it naming the old column, and every insert would
// fail on it.
func TestLiveSQLiteRebuildKeepsRenamedTrigger(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, migrate.DialectSQLite)
	bootPlan(t, db, "1", widgetsIn("widgets", []string{"id"}, nil))
	fkExec(t, db,
		`CREATE TABLE widget_log (v TEXT)`,
		`CREATE TRIGGER widgets_ins AFTER INSERT ON widgets BEGIN INSERT INTO widget_log VALUES (NEW.title); END`,
	)
	renamed := entity.Define("widgets", entity.EntityConfig{
		Table: "widgets",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "heading", Type: schema.String},
			{Name: "code", Type: schema.String},
		},
		PrimaryKey: []string{"code"},
		Renames:    map[string]string{"title": "heading"},
	}.WithTimestamps(false))
	changes, err := migrate.DiffSchema(ctx, db, fkReg{renamed})
	if err != nil {
		t.Fatal(err)
	}
	fkApply(t, db, changes)
	if got := sqliteKeyCols(t, db, "main", "widgets"); len(got) != 1 || got[0] != "code" {
		t.Fatalf("key after the apply = %v, want [code]: the rebuild did not run", got)
	}
	fkExec(t, db, `INSERT INTO widgets (id, heading, code) VALUES ('b', 'hb', 'cb')`)
	if n := countRows(t, db, `SELECT COUNT(*) FROM widget_log WHERE v = 'hb'`); n != 1 {
		t.Fatalf("the trigger logged %d rows for the insert, want 1", n)
	}
}

// SQLite fires a table's triggers for one event newest first, so the
// rebuild recreates them in the order they were made: by name, a_second
// would come back older than b_first and the two would swap.
func TestLiveSQLiteRebuildKeepsTriggerOrder(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, migrate.DialectSQLite)
	bootPlan(t, db, "1", widgetsIn("widgets", []string{"id"}, nil))
	fkExec(t, db,
		`CREATE TABLE widget_log (n INTEGER PRIMARY KEY, v TEXT)`,
		`CREATE TRIGGER b_first AFTER INSERT ON widgets BEGIN INSERT INTO widget_log (v) VALUES ('b_first'); END`,
		`CREATE TRIGGER a_second AFTER INSERT ON widgets BEGIN INSERT INTO widget_log (v) VALUES ('a_second'); END`,
	)
	fired := func() string {
		t.Helper()
		rows, err := db.Query(`SELECT v FROM widget_log ORDER BY n`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			got = append(got, v)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		fkExec(t, db, `DELETE FROM widget_log`)
		return strings.Join(got, ",")
	}
	fkExec(t, db, `INSERT INTO widgets (id, title, code) VALUES ('a', 't', 'ca')`)
	before := fired()

	changes, err := migrate.DiffSchema(ctx, db, fkReg{widgetsIn("widgets", []string{"code"}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	fkApply(t, db, changes)
	fkExec(t, db, `INSERT INTO widgets (id, title, code) VALUES ('b', 't', 'cb')`)
	if after := fired(); after != before {
		t.Fatalf("triggers fired %s after the rebuild, %s before", after, before)
	}
}

// A view that reads a table does not stop boot from rebuilding it for a
// key change. The rebuild's final RENAME re-parses every view on modern
// SQLite and fails on the dropped table, so boot runs it with
// legacy_alter_table on, as the live path and the generated file do, and
// turns it back off after (2026-10-27 M4).
func TestBootKeyRebuildWithView(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	db.SetMaxOpenConns(1) // the pragma check below reads boot's connection
	bootPlan(t, db, "1", widgetsIn("widgets", []string{"id"}, nil))
	// Boot changes only an empty table's key; the row goes in after.
	fkExec(t, db, `CREATE VIEW widget_titles AS SELECT id, title FROM widgets`)
	bootPlan(t, db, "2", widgetsIn("widgets", []string{"code"}, nil))
	fkExec(t, db, `INSERT INTO widgets (id, title, code) VALUES ('a', 't', 'ca')`)
	if got := sqliteKeyCols(t, db, "main", "widgets"); len(got) != 1 || got[0] != "code" {
		t.Fatalf("key after boot = %v, want [code]", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM widget_titles WHERE id = 'a'`); n != 1 {
		t.Fatalf("the view reads %d rows after the rebuild, want 1", n)
	}
	if n := countRows(t, db, `PRAGMA legacy_alter_table`); n != 0 {
		t.Fatalf("legacy_alter_table = %d after boot, want 0", n)
	}
}

// Boot rebuilds a SQLite table whose key changes and re-runs the declared
// routines after it, but a trigger written by hand is no routine: the
// rebuild's DROP TABLE took it and boot said nothing.
func TestBootKeyRebuildKeepsTrigger(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	bootPlan(t, db, "1", widgetsIn("widgets", []string{"id"}, nil))
	fkExec(t, db,
		`CREATE TABLE widget_log (v TEXT)`,
		`CREATE TRIGGER widgets_ins AFTER INSERT ON widgets BEGIN INSERT INTO widget_log VALUES (NEW.id); END`,
	)
	bootPlan(t, db, "2", widgetsIn("widgets", []string{"code"}, nil))
	if got := sqliteKeyCols(t, db, "main", "widgets"); len(got) != 1 || got[0] != "code" {
		t.Fatalf("key after boot = %v, want [code]: the rebuild did not run", got)
	}
	fkExec(t, db, `INSERT INTO widgets (id, title, code) VALUES ('b', 't', 'cb')`)
	if n := countRows(t, db, `SELECT COUNT(*) FROM widget_log WHERE v = 'b'`); n != 1 {
		t.Fatalf("the trigger logged %d rows after the boot rebuild, want 1", n)
	}
}
