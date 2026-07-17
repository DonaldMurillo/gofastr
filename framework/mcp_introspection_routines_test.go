package framework

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestMCPAppRoutinesListsRegisteredRoutines pins app_routines surfaces every
// registered routine with name, dialect, checksum, and ledger state.
func TestMCPAppRoutinesListsRegisteredRoutines(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Routine(migrate.Routine{Name: "f1", Up: "DROP VIEW IF EXISTS f1;\nCREATE VIEW f1 AS SELECT 1"})
	app.Routine(migrate.Routine{Name: "pg_only", Up: "CREATE OR REPLACE FUNCTION pg_only() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql", Dialect: migrate.DialectPostgres})

	// Run AutoMigrate so the ledger rows exist.
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := toolResultRoutines(t, res)
	if !strings.Contains(text, "f1") {
		t.Errorf("expected 'f1' in result; got:\n%s", text)
	}
	if !strings.Contains(text, "pg_only") {
		t.Errorf("expected 'pg_only' in result; got:\n%s", text)
	}
	if !strings.Contains(text, "postgres") {
		t.Errorf("expected dialect='postgres' for pg_only; got:\n%s", text)
	}
	// Ledger state: "present" since AutoMigrate ran.
	if !strings.Contains(text, "present") {
		t.Errorf("expected ledger state present; got:\n%s", text)
	}
}

// TestMCPAppRoutinesReportsLedgerDrift proves that when the registered Up
// drifts from the ledger checksum, the tool surfaces "drifted".
func TestMCPAppRoutinesReportsLedgerDrift(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// Apply the original body so a ledger row lands.
	app.Routine(migrate.Routine{Name: "f1", Up: "DROP VIEW IF EXISTS f1;\nCREATE VIEW f1 AS SELECT 1 AS a"})
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Tamper with the ledger row directly to simulate drift without re-applying
	// (which would update the ledger). This is the "DB says one body, code says
	// another" state the introspection tool exists to surface.
	if _, err := db.Exec("UPDATE gofastr_routines SET checksum = 'deadbeef' WHERE name = 'f1'"); err != nil {
		t.Fatal(err)
	}
	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := toolResultRoutines(t, res)
	if !strings.Contains(text, "drifted") {
		t.Errorf("expected ledger state 'drifted'; got:\n%s", text)
	}
}

// TestMCPAppRoutinesReportsMissingLedgerRow proves that when no ledger row
// exists for a registered routine (e.g. auto-migrate was skipped), the tool
// surfaces "missing".
func TestMCPAppRoutinesReportsMissingLedgerRow(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// Register a routine but DON'T auto-migrate — the ledger has no row.
	app.Routine(migrate.Routine{Name: "never_applied", Up: "DROP VIEW IF EXISTS never_applied;\nCREATE VIEW never_applied AS SELECT 1"})
	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := toolResultRoutines(t, res)
	if !strings.Contains(text, "never_applied") {
		t.Errorf("expected routine name; got:\n%s", text)
	}
	if !strings.Contains(text, "missing") {
		t.Errorf("expected ledger state 'missing'; got:\n%s", text)
	}
}

// TestMCPAppRoutinesSurvivesNilDB proves the tool returns the static fields
// (name, dialect, checksum) and an "unknown" ledger/liveness state when no DB
// is wired. Never panics.
func TestMCPAppRoutinesSurvivesNilDB(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Routine(migrate.Routine{Name: "f1", Up: "SELECT 1"})
	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := toolResultRoutines(t, res)
	if !strings.Contains(text, "f1") {
		t.Errorf("expected 'f1'; got:\n%s", text)
	}
	if !strings.Contains(text, "unknown") {
		t.Errorf("expected 'unknown' ledger/liveness state on nil DB; got:\n%s", text)
	}
}

// TestMCPAppRoutines_ListedInGuidance pins the guidance-pin contract for the
// new tool: it must be registered when WithMCPIntrospection is on.
func TestMCPAppRoutines_ListedInGuidance(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range app.MCP.ListTools() {
		names[tool.Name] = true
	}
	if !names["app_routines"] {
		t.Fatal("app_routines not registered under WithMCPIntrospection")
	}
}

// TestApp_RoutinesFS_PlaysWithIntrospection pins the full happy path:
// routines loaded from FS are visible through app_routines.
func TestApp_RoutinesFS_PlaysWithIntrospection(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	fsys := fstest.MapFS{
		"db/routines/embedded.sql": &fstest.MapFile{Data: []byte("DROP VIEW IF EXISTS embedded;\nCREATE VIEW embedded AS SELECT 1 AS x")},
	}
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.RoutinesFS(fsys, "db/routines")
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := toolResultRoutines(t, res)
	if !strings.Contains(text, "embedded") {
		t.Errorf("expected FS-loaded routine 'embedded' to show in app_routines; got:\n%s", text)
	}
}

// toolResultRoutines pulls the "routines" array out of the app_routines
// CallTool result and stringifies it for substring assertions. The framework's
// MCP CallTool returns the handler's map[string]any directly (no JSON-RPC
// content[] wrapping in-process), so callers in tests inspect structured keys.
func toolResultRoutines(t *testing.T, res any) string {
	t.Helper()
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result is not map[string]any: %T", res)
	}
	routines, _ := m["routines"].([]map[string]any)
	var b strings.Builder
	for _, r := range routines {
		fmt.Fprintf(&b, "%v\n", r)
	}
	if b.Len() == 0 {
		t.Fatalf("no routines in result: %#v", res)
	}
	return b.String()
}

// TestMCPAppRoutines_ProbesPgLiveness proves the Postgres liveness probe:
// a routine that's been applied reports liveness=present via a real
// pg_proc / pg_views lookup. Uses a real Postgres testcontainer.
func TestMCPAppRoutines_ProbesPgLiveness(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.Routine(migrate.Routine{
		Name: "live_fn",
		Up:   "CREATE OR REPLACE FUNCTION live_fn() RETURNS int AS $$ SELECT 42 $$ LANGUAGE sql",
	})
	app.Routine(migrate.Routine{
		Name: "live_view",
		Up:   "CREATE OR REPLACE VIEW live_view AS SELECT 1 AS x",
	})
	plan := migrate.Plan{Registry: app.Registry, Routines: app.migrationRoutines}
	if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := res.(map[string]any)
	routines, _ := m["routines"].([]map[string]any)
	if len(routines) != 2 {
		t.Fatalf("expected 2 routines, got %d (%v)", len(routines), routines)
	}
	for _, r := range routines {
		name, _ := r["name"].(string)
		liveness, _ := r["liveness"].(string)
		ledger, _ := r["ledger_state"].(string)
		if liveness != "present" {
			t.Errorf("%s liveness=%q want present (object should exist in pg_proc/pg_views)", name, liveness)
		}
		if ledger != "present" {
			t.Errorf("%s ledger_state=%q want present", name, ledger)
		}
	}
}

// TestMCPAppRoutines_ReportsAbsentObjectOnPG proves the liveness probe
// reports "absent" on Postgres when the registered routine was never applied
// (so the catalog has no matching pg_proc / pg_views entry).
func TestMCPAppRoutines_ReportsAbsentObjectOnPG(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	app := NewApp(WithDB(db), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// Register but DO NOT migrate — the pg_proc/pg_views lookup will come
	// back empty.
	app.Routine(migrate.Routine{
		Name: "phantom_fn",
		Up:   "CREATE OR REPLACE FUNCTION phantom_fn() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql",
	})
	// But we DO need the ledger table to exist for the ledger lookup to not
	// error — run an empty plan that creates it.
	if err := migrate.AutoMigratePlanContext(context.Background(), db,
		migrate.Plan{Registry: app.Registry, Routines: []migrate.Routine{{
			Name: "seed",
			Up:   "CREATE OR REPLACE FUNCTION seed() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql",
		}}}); err != nil {
		t.Fatalf("seed migrate: %v", err)
	}
	// Now introspect — phantom_fn is registered but never applied.
	res, err := app.MCP.CallTool(context.Background(), "app_routines", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := res.(map[string]any)
	routines, _ := m["routines"].([]map[string]any)
	for _, r := range routines {
		if name, _ := r["name"].(string); name == "phantom_fn" {
			if liveness, _ := r["liveness"].(string); liveness != "absent" {
				t.Errorf("phantom_fn liveness=%q want absent", liveness)
			}
			if ledger, _ := r["ledger_state"].(string); ledger != "missing" {
				t.Errorf("phantom_fn ledger_state=%q want missing", ledger)
			}
			return
		}
	}
	t.Errorf("phantom_fn not in result: %#v", routines)
}
