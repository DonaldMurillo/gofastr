package framework

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A raw table with no primary key has nothing to page by, so the export
// reads it whole: every row, whatever the page size.
func TestExportKeylessTableReadsAll(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Table(migrate.Table{
			Name:    "events",
			Columns: []migrate.Column{{Name: "kind", Type: schema.String}},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db, `INSERT INTO events (kind) VALUES ('a'), ('b'), ('c')`)
		dir := t.TempDir()
		if err := app.ExportData(context.Background(), dir, WithExportPageSize(1), WithExportTime(fixedExportTime())); err != nil {
			t.Fatalf("ExportData: %v", err)
		}
		rows, err := readNDJSON(filepath.Join(dir, "events.ndjson"))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 {
			t.Errorf("exported %d rows of events, want 3: %v", len(rows), rows)
		}
	})
}

// Two tables that name each other: no table order satisfies both foreign
// keys, so the restore writes one side's reference after both tables exist.
func TestImportRestoresTableCycle(t *testing.T) {
	forEachRestore(t, newCycleApp, func(t *testing.T, db *sql.DB) {
		seedCycle(t, db)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		var bID, aID sql.NullString
		if err := dst.QueryRow(`SELECT b_id FROM a_side WHERE id = 'a1'`).Scan(&bID); err != nil {
			t.Fatal(err)
		}
		if err := dst.QueryRow(`SELECT a_id FROM b_side WHERE id = 'b1'`).Scan(&aID); err != nil {
			t.Fatal(err)
		}
		if bID.String != "b1" || aID.String != "a1" {
			t.Errorf("restored a1.b_id = %q, b1.a_id = %q; want b1, a1", bID.String, aID.String)
		}
	})
}

// newCycleApp: a_side.b_id -> b_side and b_side.a_id -> a_side, both
// nullable, both owner-scoped. AutoMigrate cannot create the cycle on
// Postgres (each CREATE TABLE names a table that does not exist yet), so
// there the tables are made by hand, as a host's own migration would.
func newCycleApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("a_side", entity.EntityConfig{
		Table: "a_side",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "owner_id", Type: schema.String},
			{Name: "b_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("b", "b_side", "b_id")},
	}.WithTimestamps(false)))
	app.Registry.Register(entity.Define("b_side", entity.EntityConfig{
		Table: "b_side",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "owner_id", Type: schema.String},
			{Name: "a_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("a", "a_side", "a_id")},
	}.WithTimestamps(false)))
	if migrate.DetectDialect(db) == migrate.DialectPostgres {
		cascadeExec(t, db,
			`CREATE TABLE a_side (id TEXT PRIMARY KEY, owner_id TEXT, b_id TEXT)`,
			`CREATE TABLE b_side (id TEXT PRIMARY KEY, owner_id TEXT, a_id TEXT REFERENCES a_side(id))`,
			`ALTER TABLE a_side ADD FOREIGN KEY (b_id) REFERENCES b_side(id)`,
		)
		return app
	}
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

func seedCycle(t *testing.T, db *sql.DB) {
	t.Helper()
	cascadeExec(t, db,
		`INSERT INTO a_side (id, owner_id, b_id) VALUES ('a1','u1',NULL)`,
		`INSERT INTO b_side (id, owner_id, a_id) VALUES ('b1','u1','a1')`,
		`UPDATE a_side SET b_id = 'b1' WHERE id = 'a1'`,
	)
}

// An auto-increment table whose only id is 0: the sequence cannot be set
// below its minimum, and the restore still succeeds with the next id above
// every restored one.
func TestImportSerialBelowSequenceMin(t *testing.T) {
	forEachRestore(t, newCountersApp, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db, `INSERT INTO counters (id, label) VALUES (0, 'zero')`, `INSERT INTO counters (id, label) VALUES (-3, 'neg')`)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		var id int64
		if err := dst.QueryRow(`INSERT INTO counters (label) VALUES ('next') RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("insert after restore: %v", err)
		}
		if id < 1 {
			t.Errorf("next id after restore = %d, want one above every restored id", id)
		}
	})
}
