package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestApp_TableRegistersForMigration: App.Table puts a raw table into the
// registry so the normal migrate path creates it — no CRUD routes involved.
func TestApp_TableRegistersForMigration(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Table(migrate.Table{
			Name: "events",
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true, NotNull: true},
				{Name: "kind", Type: schema.String},
			},
		})
		if _, err := app.Registry.Get("events"); err != nil {
			t.Fatalf("App.Table did not register the table: %v", err)
		}
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cols := liveColumns(t, db, "events")
		if _, ok := cols["kind"]; !ok {
			t.Fatalf("events table not created with its columns: %v", keysOf(cols))
		}
	})
}

// TestApp_RoutineMigratesViaPlan: App.Routine accumulates a routine that the
// boot plan (the same one App.Start builds) migrates. Postgres-only (functions).
func TestApp_RoutineMigratesViaPlan(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	app := NewApp(WithDB(db))
	app.Routine(migrate.Routine{
		Name: "triple_it",
		Up:   "CREATE OR REPLACE FUNCTION triple_it(x integer) RETURNS integer AS $$ BEGIN RETURN x * 3; END; $$ LANGUAGE plpgsql",
		Down: "DROP FUNCTION IF EXISTS triple_it(integer)",
	})
	if len(app.migrationRoutines) != 1 {
		t.Fatalf("App.Routine did not record the routine, got %d", len(app.migrationRoutines))
	}
	// This mirrors exactly what App.Start runs.
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext: %v", err)
	}
	var got int
	if err := db.QueryRow("SELECT triple_it(7)").Scan(&got); err != nil {
		t.Fatalf("call routine: %v", err)
	}
	if got != 21 {
		t.Fatalf("triple_it(7) = %d, want 21", got)
	}
}
