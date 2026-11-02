package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A generated SQLite rebuild drops the table, and with foreign keys on
// SQLite runs an implicit DELETE FROM first. A child holding a plain
// foreign key (no ON DELETE action) onto the table then fails the DROP, so
// the file applied on a database whose child was empty and failed with
// "FOREIGN KEY constraint failed" on every database where it held a row
// (2026-11-01 bug bash). Generation refuses it, as it refuses a rebuild
// that CASCADE or SET NULL keys reference.
func TestGeneratedRebuildOfReferencedTableRefused(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectSQLite {
			t.Skip("Postgres retypes in place; nothing is dropped")
		}
		parent := func(tp schema.FieldType) *entity.Entity {
			return entity.Define("rp", entity.EntityConfig{
				Table:      "rp",
				Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "n", Type: tp}},
				PrimaryKey: []string{"id"},
			}.WithTimestamps(false))
		}
		child := entity.Define("rc", entity.EntityConfig{
			Table:      "rc",
			Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "p_id", Type: schema.String}},
			PrimaryKey: []string{"id"},
			Relations:  []entity.Relation{entity.BelongsTo("rp", "rp", "p_id")},
		}.WithTimestamps(false))

		up1, _, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.Int), child}}, migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		m := coremig.New(db, coremig.WithDialect(d))
		if err := m.Register(coremig.Migration{Version: 1, Name: "create", Up: up1}); err != nil {
			t.Fatal(err)
		}
		if err := m.Up(context.Background()); err != nil {
			t.Fatal(err)
		}
		fkExec(t, db, "INSERT INTO rp (id, n) VALUES ('p1', 5)", "INSERT INTO rc (id, p_id) VALUES ('c1', 'p1')")

		up2, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.String), child}}, snap, d)
		if err != nil {
			if !strings.Contains(err.Error(), "rc") || !strings.Contains(err.Error(), "foreign_keys=OFF") {
				t.Fatalf("refusal does not name the referencing table and the way out: %v", err)
			}
			return
		}
		if err := m.Register(coremig.Migration{Version: 2, Name: "retype", Up: up2}); err != nil {
			t.Fatal(err)
		}
		if err := m.Up(context.Background()); err != nil {
			t.Fatalf("the generated rebuild does not apply while the child holds a row: %v", err)
		}
	})
}
