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

// Two columns made Unique on an existing table whose <table>_<col>_key
// names share their first 63 bytes both get a unique index. Postgres cut
// both names to the same 63 bytes, so CREATE UNIQUE INDEX IF NOT EXISTS
// skipped the second and its column took duplicates (round 3 review of
// M7). Two long column names that Postgres's trim cuts to one prefix (60
// x's then "a" or "b") collided the same way under the trimmed name, so
// a trimmed name carries a hash of the whole (round 3 re-review).
func TestLongUniqueKeyNamesStayDistinct(t *testing.T) {
	for _, tc := range []struct{ table, c1, c2 string }{
		{"t" + strings.Repeat("x", 57), "email", "email_alt"},
		{"t", strings.Repeat("x", 60) + "a", strings.Repeat("x", 60) + "b"},
	} {
		testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
			table, c1, c2 := tc.table, tc.c1, tc.c2
			def := func(unique bool) *entity.Entity {
				return entity.Define("accounts", entity.EntityConfig{
					Table: table,
					Fields: []schema.Field{
						{Name: c1, Type: schema.String, Unique: unique},
						{Name: c2, Type: schema.String, Unique: unique},
					},
				}.WithTimestamps(false))
			}
			bootPlan(t, db, "1", def(false))
			bootPlan(t, db, "2", def(true))
			ins := "INSERT INTO " + table + " (id, " + c1 + ", " + c2 + ") VALUES "
			fkExec(t, db, ins+"('a', 'a@x', 'same')")
			if _, err := db.Exec(ins + "('b', 'b@x', 'same')"); err == nil {
				t.Errorf("%s took a duplicate: its unique index was never created", c2)
			}
			if _, err := db.Exec(ins + "('c', 'a@x', 'other')"); err == nil {
				t.Errorf("%s took a duplicate: its unique index was never created", c1)
			}
		})
	}
}

// The inline UNIQUE constraints CREATE TABLE writes carry the names
// entity.UniqueKeyName gives, or a violation maps to no field. Up to 63
// bytes that is the name Postgres picks itself. Past 63 bytes CREATE
// TABLE names the constraint: Postgres trims two long columns to one name
// and numbers the second, which no per-column name can predict.
func TestUniqueKeyNameMatchesPostgres(t *testing.T) {
	for _, tc := range []struct{ table, c1, c2 string }{
		{"u", "email", "email_alt"},
		{"u" + strings.Repeat("y", 57), "email", "email_alt"},
		{"u", strings.Repeat("y", 60) + "a", strings.Repeat("y", 60) + "b"},
	} {
		testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
			if d != migrate.DialectPostgres {
				t.Skip("Postgres names the inline constraint")
			}
			bootPlan(t, db, "1", entity.Define("accounts", entity.EntityConfig{
				Table: tc.table,
				Fields: []schema.Field{
					{Name: tc.c1, Type: schema.String, Unique: true},
					{Name: tc.c2, Type: schema.String, Unique: true},
				},
			}.WithTimestamps(false)))
			got := pgUniqueNames(t, db, tc.table)
			want := []string{entity.UniqueKeyName(tc.table, tc.c1), entity.UniqueKeyName(tc.table, tc.c2)}
			slices.Sort(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("Postgres named the constraints %v, UniqueKeyName gives %v", got, want)
			}
		})
	}
}

// A table a build before v0.96.0 created holds its long-named UNIQUE
// constraints under the names Postgres chose: the trimmed name, then a
// numbered one for the second column trimmed to it. Clearing Unique drops
// the column's constraint whatever its name, found in the catalog when
// the migration runs; dropping by today's name dropped nothing (round 3
// re-review).
func TestClearDropsLegacyUniqueName(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("SQLite clears Unique by rebuilding the table")
		}
		table := "v"
		c1, c2 := strings.Repeat("z", 60)+"a", strings.Repeat("z", 60)+"b"
		def := func(u2 bool) *entity.Entity {
			return entity.Define("accounts", entity.EntityConfig{
				Table: table,
				Fields: []schema.Field{
					{Name: c1, Type: schema.String, Unique: true},
					{Name: c2, Type: schema.String, Unique: u2},
				},
			}.WithTimestamps(false))
		}
		// The table as v0.95.0 made it: Postgres named both constraints.
		fkExec(t, db, "CREATE TABLE "+table+" (id TEXT PRIMARY KEY, "+c1+" TEXT UNIQUE, "+c2+" TEXT UNIQUE)")
		if got := pgUniqueNames(t, db, table); len(got) != 2 || !strings.HasSuffix(got[0], "_key1") && !strings.HasSuffix(got[1], "_key1") {
			t.Fatalf("Postgres named the constraints %v, want one numbered _key1", got)
		}
		prev := migrate.SnapshotFromRegistry(fkReg{def(true)}, d)
		up, _, _, err := migrate.GenerateMigration(fkReg{def(false)}, prev, d)
		if err != nil {
			t.Fatal(err)
		}
		fkExec(t, db, up)
		ins := "INSERT INTO " + table + " (id, " + c1 + ", " + c2 + ") VALUES "
		fkExec(t, db, ins+"('a', 'x', 'same')")
		if _, err := db.Exec(ins + "('b', 'y', 'same')"); err != nil {
			t.Errorf("%s is no longer Unique and still refuses a duplicate: %v\nmigration:\n%s", c2, err, up)
		}
		if _, err := db.Exec(ins + "('c', 'x', 'other')"); err == nil {
			t.Errorf("%s took a duplicate: the clear of the other column dropped its constraint", c1)
		}
	})
}

// The clear finds the column by the name unquoted DDL gives it, folded,
// and compares the catalog's name exactly: a quoted "Email" beside email
// is another column, and lowering the catalog side dropped its UNIQUE
// too (round 3 final review).
func TestClearKeepsQuotedCaseTwin(t *testing.T) {
	for _, field := range []string{"email", "EMAIL"} {
		clearBesideCaseTwin(t, field)
	}
}

func clearBesideCaseTwin(t *testing.T, field string) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("SQLite clears Unique by rebuilding the table")
		}
		def := func(unique bool) *entity.Entity {
			return entity.Define("people", entity.EntityConfig{
				Table:  "people",
				Fields: []schema.Field{{Name: field, Type: schema.String, Unique: unique}},
			}.WithTimestamps(false))
		}
		fkExec(t, db, `CREATE TABLE people (id TEXT PRIMARY KEY, email TEXT UNIQUE, "Email" TEXT UNIQUE)`)
		prev := migrate.SnapshotFromRegistry(fkReg{def(true)}, d)
		up, _, _, err := migrate.GenerateMigration(fkReg{def(false)}, prev, d)
		if err != nil {
			t.Fatal(err)
		}
		fkExec(t, db, up)
		fkExec(t, db, `INSERT INTO people (id, email, "Email") VALUES ('a', 'same', 'x')`)
		if _, err := db.Exec(`INSERT INTO people (id, email, "Email") VALUES ('b', 'same', 'y')`); err != nil {
			t.Errorf("email is no longer Unique and still refuses a duplicate: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO people (id, email, "Email") VALUES ('c', 'other', 'x')`); err == nil {
			t.Error(`"Email" took a duplicate: clearing email dropped its constraint`)
		}
	})
}

// Table t_a column b and table t column a_b both name their unique index
// t_a_b_key, the name Postgres gives the inline constraint, and index
// names share a schema. Boot made the first and CREATE INDEX IF NOT
// EXISTS skipped the second, so a_b took duplicates while boot reported
// success. Boot creates the index only for a column no unique index
// covers, so a taken name now fails it, naming the column.
func TestBootRefusesTakenUniqueName(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		def := func(name, table, col string, unique bool) *entity.Entity {
			return entity.Define(name, entity.EntityConfig{
				Table:  table,
				Fields: []schema.Field{{Name: col, Type: schema.String, Unique: unique}},
			}.WithTimestamps(false))
		}
		bootPlan(t, db, "1", def("one", "t_a", "b", false), def("two", "t", "a_b", false))
		err := migrate.AutoMigratePlanContext(context.Background(), db, migrate.Plan{Registry: fkReg{def("one", "t_a", "b", true), def("two", "t", "a_b", true)}})
		if err != nil {
			if !strings.Contains(err.Error(), "a_b") && !strings.Contains(err.Error(), ".b ") {
				t.Errorf("boot failed without naming the column: %v", err)
			}
			return
		}
		fkExec(t, db, "INSERT INTO t (id, a_b) VALUES ('1', 'same')")
		if _, err := db.Exec("INSERT INTO t (id, a_b) VALUES ('2', 'same')"); err == nil {
			t.Error("boot succeeded and t.a_b took a duplicate: its unique index was skipped")
		}
		fkExec(t, db, "INSERT INTO t_a (id, b) VALUES ('1', 'same')")
		if _, err := db.Exec("INSERT INTO t_a (id, b) VALUES ('2', 'same')"); err == nil {
			t.Error("boot succeeded and t_a.b took a duplicate: its unique index was skipped")
		}
	})
}

// pgUniqueNames lists the UNIQUE constraints on table, sorted.
func pgUniqueNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	return queryStrings(t, db, "SELECT conname FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'u' ORDER BY conname", table)
}
