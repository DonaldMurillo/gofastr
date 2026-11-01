package migrate_test

import (
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
// M7).
func TestLongUniqueKeyNamesStayDistinct(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		table := "t" + strings.Repeat("x", 57)
		def := func(unique bool) *entity.Entity {
			return entity.Define("accounts", entity.EntityConfig{
				Table: table,
				Fields: []schema.Field{
					{Name: "email", Type: schema.String, Unique: unique},
					{Name: "email_alt", Type: schema.String, Unique: unique},
				},
			}.WithTimestamps(false))
		}
		bootPlan(t, db, "1", def(false))
		bootPlan(t, db, "2", def(true))
		fkExec(t, db, "INSERT INTO "+table+" (id, email, email_alt) VALUES ('a', 'a@x', 'same')")
		if _, err := db.Exec("INSERT INTO " + table + " (id, email, email_alt) VALUES ('b', 'b@x', 'same')"); err == nil {
			t.Error("email_alt took a duplicate: its unique index was never created")
		}
		if _, err := db.Exec("INSERT INTO " + table + " (id, email, email_alt) VALUES ('c', 'a@x', 'other')"); err == nil {
			t.Error("email took a duplicate: its unique index was never created")
		}
	})
}

// The inline UNIQUE constraints CREATE TABLE writes on a long table are
// named by Postgres itself; entity.UniqueKeyName must give the same
// names, or a clear drops nothing and a violation maps to no field.
func TestUniqueKeyNameMatchesPostgres(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("Postgres names the inline constraint")
		}
		table := "u" + strings.Repeat("y", 57)
		bootPlan(t, db, "1", entity.Define("accounts", entity.EntityConfig{
			Table: table,
			Fields: []schema.Field{
				{Name: "email", Type: schema.String, Unique: true},
				{Name: "email_alt", Type: schema.String, Unique: true},
			},
		}.WithTimestamps(false)))
		rows, err := db.Query("SELECT conname FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'u' ORDER BY conname", table)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			got = append(got, n)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := []string{entity.UniqueKeyName(table, "email"), entity.UniqueKeyName(table, "email_alt")}
		slices.Sort(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("Postgres named the constraints %v, UniqueKeyName gives %v", got, want)
		}
	})
}
