package framework

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestApp_TableRegistersForMigration: App.Table puts a raw table into the
// registry so the normal migrate path creates it, no CRUD routes involved.
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

// TestApp_MigrationPlanExposesRegistry: app.MigrationPlan() returns the exact
// Plan App.Start auto-migrates from, the registry plus every Routine/View
// registered, so the host-binary generate path (migrate.GenerateMigrationFile)
// diffs the same compiled schema boot applies. DB-free: it is a pure accessor.
func TestApp_MigrationPlanExposesRegistry(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.Routine(migrate.Routine{Name: "triple_it", Up: "SELECT 1", Down: "DROP"})
	// No Columns: this view is migration-only, which is all the plan accessor
	// needs to expose. Declaring Columns without a PrimaryKey panics, since the
	// ORM would have no id to read.
	app.View(migrate.View{Name: "v", Select: "SELECT 1 AS x"})

	plan := app.MigrationPlan()
	if plan.Registry == nil {
		t.Fatal("MigrationPlan: nil registry")
	}
	if _, err := plan.Registry.Get("posts"); err != nil {
		t.Errorf("registry missing posts: %v", err)
	}
	if len(plan.Routines) != 1 || plan.Routines[0].Name != "triple_it" {
		t.Errorf("MigrationPlan routines = %+v, want [triple_it]", plan.Routines)
	}
	if len(plan.Views) != 1 || plan.Views[0].Name != "v" {
		t.Errorf("MigrationPlan views = %+v, want [v]", plan.Views)
	}
}

// TestApp_GenerateMigrationFileFromPlan walks the path a graduated app takes:
// registry compiled into the binary → MigrationPlan → a versioned migration on
// disk, through the framework re-export rather than framework/migrate directly.
func TestApp_GenerateMigrationFileFromPlan(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
	}.WithTimestamps(false))

	dir := t.TempDir()
	path, err := GenerateMigrationFile(app.MigrationPlan(), "create posts", MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		Dialect:       DialectSQLite,
	})
	if err != nil {
		t.Fatalf("GenerateMigrationFile: %v", err)
	}
	if want := filepath.Join(dir, "migrations", "0001_create_posts.sql"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if !strings.Contains(string(body), "CREATE TABLE") ||
		!strings.Contains(string(body), "posts") {
		t.Errorf("migration missing CREATE TABLE posts:\n%s", body)
	}
	// The snapshot defaults to <MigrationsDir>/schema.snapshot.json.
	if _, err := os.Stat(filepath.Join(dir, "migrations", "schema.snapshot.json")); err != nil {
		t.Errorf("snapshot not written: %v", err)
	}
}
