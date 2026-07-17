package framework

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestApp_RoutinesFS_RegistersAllDialects proves App.RoutinesFS parses the
// directory once and forwards each Routine to App.Routine (i.e. they show up
// in the plan App.Start builds).
func TestApp_RoutinesFS_RegistersAllDialects(t *testing.T) {
	fsys := fstest.MapFS{
		"db/routines/any.sql":        &fstest.MapFile{Data: []byte("CREATE VIEW any_v AS SELECT 1")},
		"db/routines/pg_only.pg.sql": &fstest.MapFile{Data: []byte("CREATE OR REPLACE FUNCTION pg_fn() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql")},
	}
	app := NewApp()
	app.RoutinesFS(fsys, "db/routines")
	names := make([]string, 0, len(app.migrationRoutines))
	for _, r := range app.migrationRoutines {
		names = append(names, r.Name)
	}
	if len(names) != 2 || !containsName(names, "any") || !containsName(names, "pg_only") {
		t.Fatalf("App.RoutinesFS registered %v, want [any pg_only]", names)
	}
	// Spot-check the dialect tag survives through App registration.
	for _, r := range app.migrationRoutines {
		if r.Name == "pg_only" && r.Dialect != migrate.DialectPostgres {
			t.Errorf("pg_only dialect lost: got %q want %q", r.Dialect, migrate.DialectPostgres)
		}
	}
}

// TestApp_RoutinesFS_PanicsOnEmptyDir proves the scream-on-misconfig contract
// — a path that resolves to no .sql files aborts App construction.
func TestApp_RoutinesFS_PanicsOnEmptyDir(t *testing.T) {
	fsys := fstest.MapFS{
		"db/routines/README.md": &fstest.MapFile{Data: []byte("nope")},
	}
	app := NewApp()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on empty routines dir, got none")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "db/routines") {
			t.Errorf("panic should name the dir; got %v", r)
		}
	}()
	app.RoutinesFS(fsys, "db/routines")
}

// TestApp_RoutinesFS_EndToEndOnSQLite proves the full boot path: load from FS,
// register, and AutoMigrate against a SQLite DB actually creates the routines.
// (App.Start is too heavy here — we mirror what Start does for routines.)
func TestApp_RoutinesFS_EndToEndOnSQLite(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	fsys := fstest.MapFS{
		"db/routines/v.sql": &fstest.MapFile{Data: []byte("DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT 1 AS x")},
	}
	app := NewApp(WithDB(db))
	app.RoutinesFS(fsys, "db/routines")
	if len(app.migrationRoutines) != 1 {
		t.Fatalf("routine not registered: got %d", len(app.migrationRoutines))
	}
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='view' AND name='v'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("view not created: count=%d", n)
	}
	// Ledger row landed.
	var ln int
	if err := db.QueryRow("SELECT COUNT(*) FROM gofastr_routines WHERE name='v'").Scan(&ln); err != nil {
		t.Fatal(err)
	}
	if ln != 1 {
		t.Fatalf("ledger row not created: count=%d", ln)
	}
}

func containsName(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
