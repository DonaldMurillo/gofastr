package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Removing a routine that declares no Down cannot be generated: the drop
// is unknown. Generate refuses instead of writing a ";" migration and
// advancing the snapshot past a routine still in the database
// (2026-10-27 M3). The way out: declare it again with a Down, generate
// to record it, then remove it.
func TestRoutineRemovalWithoutDownRefused(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{MigrationsDir: dir, Dialect: DialectSQLite}
	fn := Routine{Name: "touch", Up: "CREATE VIEW IF NOT EXISTS touch AS SELECT 1"}
	if _, err := GenerateMigrationFile(Plan{Routines: []Routine{fn}}, "add", opts); err != nil {
		t.Fatal(err)
	}
	snapPath := filepath.Join(dir, "schema.snapshot.json")
	before, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatal(err)
	}

	path, err := GenerateMigrationFile(Plan{}, "remove", opts)
	if err == nil || !strings.Contains(err.Error(), `routine "touch"`) || !strings.Contains(err.Error(), "Down") {
		t.Fatalf("remove without Down: path %q, err %v; want a refusal naming the routine", path, err)
	}
	if after, _ := os.ReadFile(snapPath); string(after) != string(before) {
		t.Fatal("the snapshot advanced past a routine that was never dropped")
	}

	// Declaring the same Up with a Down records it. Here the delta equals
	// migration 1 byte for byte, so the crash reconcile saves the
	// snapshot without a second file; either way the Down is recorded.
	fn.Down = "DROP VIEW IF EXISTS touch"
	if _, err = GenerateMigrationFile(Plan{Routines: []Routine{fn}}, "record_down", opts); err != nil {
		t.Fatalf("record Down: %v", err)
	}
	if snap, _ := os.ReadFile(snapPath); !strings.Contains(string(snap), "DROP VIEW IF EXISTS touch") {
		t.Fatalf("the Down was not recorded:\n%s", snap)
	}
	// ... and the removal then carries the drop.
	path, err = GenerateMigrationFile(Plan{}, "remove", opts)
	if err != nil || path == "" {
		t.Fatalf("remove with Down: path %q, err %v", path, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(body), "-- +migrate Down")
	if !strings.Contains(up, "DROP VIEW IF EXISTS touch") {
		t.Fatalf("removal migration has no drop:\n%s", body)
	}
}

// An unchanged routine produces no change, with or without a Down, and a
// snapshot view with no recorded Down is refused on removal like a
// routine.
func TestRoutineDownOnlyMattersWhenNew(t *testing.T) {
	keep := Routine{Name: "r", Up: "CREATE VIEW IF NOT EXISTS r AS SELECT 1", Down: "DROP VIEW IF EXISTS r"}
	prev := SchemaSnapshot{Routines: map[string]RoutineDef{"r": {Up: keep.Up, Down: keep.Down}}}
	if up, _, _, err := GeneratePlan(Plan{Routines: []Routine{keep}}, prev, DialectSQLite); err != nil || up != "" {
		t.Fatalf("unchanged routine: up %q, err %v", up, err)
	}
	bare := Routine{Name: "r", Up: keep.Up}
	prev.Routines["r"] = RoutineDef{Up: keep.Up}
	if up, _, _, err := GeneratePlan(Plan{Routines: []Routine{bare}}, prev, DialectSQLite); err != nil || up != "" {
		t.Fatalf("unchanged routine without Down: up %q, err %v", up, err)
	}

	views := SchemaSnapshot{Views: map[string]RoutineDef{"v": {Up: "CREATE VIEW v AS SELECT 1"}}}
	if _, _, _, err := GeneratePlan(Plan{}, views, DialectSQLite); err == nil || !strings.Contains(err.Error(), `"v"`) {
		t.Fatalf("view removal without Down: err %v, want a refusal", err)
	}
}
