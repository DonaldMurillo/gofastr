package migrate_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Required or Default set or cleared on a column that already existed
// made no change: the snapshot compared column types only, so a field
// made optional kept its NOT NULL and a new default never reached the
// table (2026-10-27 bug bash M15).

func noteField(required bool, def any) []schema.Field {
	return []schema.Field{{Name: "note", Type: schema.String, Required: required, Default: def}}
}

func notePlan(required bool, def any) migrate.Plan {
	return migrate.Plan{Registry: fkReg{ixEntity(noteField(required, def))}}
}

// acceptsNull reports whether a row with a NULL note inserts.
func acceptsNull(t *testing.T, db *sql.DB) bool {
	t.Helper()
	_, err := db.Exec("INSERT INTO ixt (id, note) VALUES ('n1', NULL)")
	fkExec(t, db, "DELETE FROM ixt")
	return err == nil
}

// noteDefault is the note a row inserted without one gets.
func noteDefault(t *testing.T, db *sql.DB) sql.NullString {
	t.Helper()
	fkExec(t, db, "INSERT INTO ixt (id) VALUES ('d1')")
	var got sql.NullString
	if err := db.QueryRow("SELECT note FROM ixt WHERE id = 'd1'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	fkExec(t, db, "DELETE FROM ixt")
	return got
}

func TestGenerateConvergesRequiredFlip(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d, notePlan(false, nil), notePlan(true, nil))
		if acceptsNull(t, db) {
			t.Fatal("Required set on an existing column still accepts NULL")
		}
		if d == migrate.DialectPostgres {
			genDown(t, db, d, migs)
			if !acceptsNull(t, db) {
				t.Fatal("rolling back Required left the column NOT NULL")
			}
		}
	})
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d, notePlan(true, nil), notePlan(false, nil))
		if !acceptsNull(t, db) {
			t.Fatal("Required cleared on an existing column still refuses NULL")
		}
		if d == migrate.DialectPostgres {
			genDown(t, db, d, migs)
			if acceptsNull(t, db) {
				t.Fatal("rolling back the clear did not restore NOT NULL")
			}
		}
	})
}

func TestGenerateConvergesDefaultChange(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d, notePlan(false, "draft"), notePlan(false, "open"))
		if got := noteDefault(t, db); got.String != "open" {
			t.Fatalf("default after the change = %v, want open", got)
		}
		if d == migrate.DialectPostgres {
			genDown(t, db, d, migs)
			if got := noteDefault(t, db); got.String != "draft" {
				t.Fatalf("default after rollback = %v, want draft", got)
			}
		}
	})
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d, notePlan(false, "draft"), notePlan(false, nil))
		if got := noteDefault(t, db); got.Valid {
			t.Fatalf("default cleared, a row still gets %q", got.String)
		}
		if d == migrate.DialectPostgres {
			genDown(t, db, d, migs)
			if got := noteDefault(t, db); got.String != "draft" {
				t.Fatalf("default after rollback = %v, want draft", got)
			}
		}
	})
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		genChain(t, db, d, notePlan(false, nil), notePlan(false, "open"))
		if got := noteDefault(t, db); got.String != "open" {
			t.Fatalf("default set on an existing column = %v, want open", got)
		}
	})
}

// A renamed column is known to the snapshot by its old name.
func TestGenerateDefaultAcrossRename(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		renamed := ixEntity(noteField(false, "open"))
		renamed.Config.Renames = map[string]string{"old": "note"}
		genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{{Name: "old", Type: schema.String, Default: "draft"}})}},
			migrate.Plan{Registry: fkReg{renamed}},
		)
		if got := noteDefault(t, db); got.String != "open" {
			t.Fatalf("default changed in a rename = %v, want open", got)
		}
	})
}

// An unchanged entity makes no change, even when a default's literal
// holds the separator the CREATE TABLE joins columns with, a quote, or
// the words NOT NULL and DEFAULT.
func TestGenerateUnchangedDefaultIsQuiet(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectPostgres, migrate.DialectSQLite} {
		plan := notePlan(false, "it's,\n\tz NOT NULL DEFAULT 'x'")
		_, _, snap, err := migrate.GeneratePlan(plan, migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _, err := migrate.GeneratePlan(plan, snap, d)
		if err != nil {
			t.Fatal(err)
		}
		if up != "" {
			t.Fatalf("%s: an unchanged entity generated:\n%s", d, up)
		}
	}
}

// A column the recorded CREATE TABLE does not define (a snapshot edited
// by hand, or one whose TableDDL fell behind its column sets) is not
// compared against an empty definition.
func TestUnrecordedColumnIsNotCompared(t *testing.T) {
	plan := notePlan(true, nil)
	_, _, snap, err := migrate.GeneratePlan(plan, migrate.SchemaSnapshot{}, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	ddl := snap.TableDDL["ixt"]
	trimmed := strings.Replace(ddl, ",\n\tnote TEXT NOT NULL", "", 1)
	if trimmed == ddl {
		t.Fatalf("fixture: no note definition in\n%s", ddl)
	}
	snap.TableDDL["ixt"] = trimmed
	up, _, _, err := migrate.GeneratePlan(plan, snap, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if up != "" {
		t.Fatalf("an unrecorded column was compared:\n%s", up)
	}
}

// A column that joins or leaves the key takes its NOT NULL from the key
// change, whose Down drops the new key before clearing it. A second
// DROP NOT NULL in the same Down ran first, on a column the new key
// still held, and Postgres refused it.
func TestKeyChangeOwnsKeyNotNull(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("SQLite rebuilds the table and generates no Down")
		}
		keyed := func(key ...string) migrate.Plan {
			ent := ixEntity(noteField(false, nil))
			ent.Config.PrimaryKey = key
			return migrate.Plan{Registry: fkReg{ent}}
		}
		migs := genChain(t, db, d, keyed("id"), keyed("id", "note"))
		genDown(t, db, d, migs)
		if !acceptsNull(t, db) {
			t.Fatal("rolling back the key change left note NOT NULL")
		}
	})
}

// A Postgres text-to-integer retype drops the default and sets the
// declared one itself; the default diff does not set it a second time,
// and leaves no Down that writes the old text default on the integer
// column.
func TestRetypeOwnsItsDefault(t *testing.T) {
	_, _, snap, err := migrate.GeneratePlan(notePlan(false, "7"), migrate.SchemaSnapshot{}, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	next := migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{{Name: "note", Type: schema.Int, Default: 5}})}}
	up, _, _, err := migrate.GeneratePlan(next, snap, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(up, "SET DEFAULT"); n != 1 {
		t.Fatalf("retype with a new default sets it %d times:\n%s", n, up)
	}
}
