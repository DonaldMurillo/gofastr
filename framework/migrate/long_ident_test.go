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

// Postgres cuts an identifier past 63 bytes to its first 63, in DDL and in
// queries, so CRUD works on such a name while the catalog holds the cut
// one. The migrator compared the whole name with the catalog: a long table
// read as absent on every boot and never gained a column, and a long
// column read as missing, so the next boot with anything to add ran ADD
// COLUMN for it, which Postgres refused as a duplicate, and DiffSchema
// proposed dropping it (2026-11-01 bug bash).

var (
	longCol   = "note_" + strings.Repeat("x", 70)
	longKey   = "key_" + strings.Repeat("k", 70)
	longTable = "tbl_" + strings.Repeat("t", 70)
)

func longEntity(table string, extra bool) *entity.Entity {
	fields := []schema.Field{
		{Name: longKey, Type: schema.String, Required: true},
		{Name: longCol, Type: schema.String, Unique: true},
	}
	if extra {
		fields = append(fields, schema.Field{Name: "added", Type: schema.String})
	}
	return entity.Define("lng", entity.EntityConfig{
		Table:      table,
		Fields:     fields,
		PrimaryKey: []string{longKey},
	}.WithTimestamps(false))
}

// A long column made Unique on an existing table gets its unique index at
// boot, once: the column read as absent, so boot skipped the index, and a
// reboot that did not see the index cover it would create it again.
func TestLongColumnMadeUniqueAtBoot(t *testing.T) {
	ent := func(unique bool) *entity.Entity {
		return entity.Define("lnu", entity.EntityConfig{
			Table: "lnu",
			Fields: []schema.Field{
				{Name: "id", Type: schema.String, Required: true},
				{Name: longCol, Type: schema.String, Unique: unique},
			},
			PrimaryKey: []string{"id"},
		}.WithTimestamps(false))
	}
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if err := migrate.AutoMigrate(db, fkReg{ent(false)}); err != nil {
			t.Fatalf("first boot: %v", err)
		}
		fkExec(t, db, "INSERT INTO lnu (id, "+longCol+") VALUES ('a', 'v')")
		for i := range 2 {
			if err := migrate.AutoMigrate(db, fkReg{ent(true)}); err != nil {
				t.Fatalf("boot %d with the column Unique: %v", i+2, err)
			}
		}
		if _, err := db.Exec("INSERT INTO lnu (id, " + longCol + ") VALUES ('b', 'v')"); err == nil {
			t.Fatal("the long column made Unique takes duplicates")
		}
	})
}

func TestLongIdentifiersConverge(t *testing.T) {
	for _, table := range []string{"lng", longTable} {
		testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
			if err := migrate.AutoMigrate(db, fkReg{longEntity(table, false)}); err != nil {
				t.Fatalf("first boot: %v", err)
			}
			fkExec(t, db, "INSERT INTO "+table+" ("+longKey+", "+longCol+") VALUES ('k1', 'kept')")
			if err := migrate.AutoMigrate(db, fkReg{longEntity(table, true)}); err != nil {
				t.Fatalf("boot adding a column: %v", err)
			}
			if _, err := db.Exec("INSERT INTO " + table + " (" + longKey + ", " + longCol + ", added) VALUES ('k2', 'new', 'a')"); err != nil {
				t.Fatalf("the column the second boot declared was not added: %v", err)
			}
			if _, err := db.Exec("INSERT INTO " + table + " (" + longKey + ", " + longCol + ") VALUES ('k3', 'kept')"); err == nil {
				t.Fatal("the long Unique column takes duplicates")
			}
			var got string
			if err := db.QueryRow("SELECT " + longCol + " FROM " + table + " WHERE " + longKey + " = 'k1'").Scan(&got); err != nil || got != "kept" {
				t.Fatalf("the first row did not survive: %q %v", got, err)
			}
			changes, err := migrate.DiffSchema(context.Background(), db, fkReg{longEntity(table, true)})
			if err != nil || len(changes) != 0 {
				t.Fatalf("DiffSchema on a converged schema = %v, %v; want none", changes, err)
			}
			if err := migrate.AutoMigrate(db, fkReg{longEntity(table, true)}); err != nil {
				t.Fatalf("an idle reboot: %v", err)
			}
		})
	}
}
