package migrate_test

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A SQLite retype rebuilds the table, and DROP TABLE takes the table's
// triggers with it. A trigger declared in Plan.Routines that did not
// change emitted nothing, so the generated migration left it gone while
// the snapshot still recorded it.
func TestSQLiteRebuildKeepsTrigger(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	trig := migrate.Routine{
		Name: "qty_touch",
		Up:   "CREATE TRIGGER IF NOT EXISTS qty_touch AFTER INSERT ON qty BEGIN SELECT 1; END",
		Down: "DROP TRIGGER IF EXISTS qty_touch",
	}
	genChain(t, db, migrate.DialectSQLite,
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.String})}, Routines: []migrate.Routine{trig}},
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.Int})}, Routines: []migrate.Routine{trig}},
	)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'qty_touch'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("trigger qty_touch is gone after the retype rebuild")
	}
}

// A modern SQLite RENAME re-resolves every view naming the table, and at
// the rename the original is already dropped, so the generated rebuild
// failed with "error in view qty_v: no such table". Boot runs the rebuild
// with legacy_alter_table on; the generated file now does too.
func TestSQLiteRebuildUnderView(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	v := []migrate.View{{Name: "qty_v", Select: "SELECT id, n FROM qty"}}
	genChain(t, db, migrate.DialectSQLite,
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.String})}, Views: v},
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.Int})}, Views: v},
	)
	fkExec(t, db, `INSERT INTO qty (id, n) VALUES ('r', 7)`)
	var n int
	if err := db.QueryRow(`SELECT n FROM qty_v WHERE id = 'r'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("view read n = %d, want 7", n)
	}
	assertLegacyAlterOff(t, db)
}

// The same rename failure from a trigger on another table whose body
// names the rebuilt one.
func TestSQLiteRebuildUnderCrossTrigger(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	other := ixEntity(nil)
	trig := migrate.Routine{
		Name: "ixt_clears_qty",
		Up:   "CREATE TRIGGER IF NOT EXISTS ixt_clears_qty AFTER INSERT ON ixt BEGIN DELETE FROM qty WHERE id = NEW.id; END",
		Down: "DROP TRIGGER IF EXISTS ixt_clears_qty",
	}
	genChain(t, db, migrate.DialectSQLite,
		migrate.Plan{Registry: fkReg{other, qtyEntity(schema.Field{Name: "n", Type: schema.String})}, Routines: []migrate.Routine{trig}},
		migrate.Plan{Registry: fkReg{other, qtyEntity(schema.Field{Name: "n", Type: schema.Int})}, Routines: []migrate.Routine{trig}},
	)
	fkExec(t, db, `INSERT INTO qty (id, n) VALUES ('r', 1)`)
	fkExec(t, db, `INSERT INTO ixt (id) VALUES ('r')`)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM qty`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("trigger ixt_clears_qty did not fire after the rebuild")
	}
	assertLegacyAlterOff(t, db)
}

// assertLegacyAlterOff fails when the migration left legacy_alter_table
// on: every later rename on the connection would skip rewriting the
// views and triggers that name the table.
func assertLegacyAlterOff(t *testing.T, db *sql.DB) {
	t.Helper()
	var on int
	if err := db.QueryRow(`PRAGMA legacy_alter_table`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 0 {
		t.Fatal("legacy_alter_table is still on after the migration")
	}
}
