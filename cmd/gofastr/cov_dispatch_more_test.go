package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDispatchRoutesNonBlocking exercises every dispatch case that does
// not block on a serve loop. Each runs with args that terminate quickly
// (help/usage/unknown-subcommand paths), so we only assert dispatch
// routed without crashing the suite.
func TestDispatchRoutesNonBlocking(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)

	covT_capStdout(t, func() { dispatch([]string{"theme"}) })
	covT_capStdout(t, func() { dispatch([]string{"new", "-h"}) })
	covT_capStdout(t, func() { dispatch([]string{"docs", "--help"}) })
	covT_capStdout(t, func() { dispatch([]string{"embed", "help"}) })

	// These dispatch to functions whose no-/bad-arg path — or a
	// removed-feature path (`generate entity`) — calls osExit.
	for _, args := range [][]string{
		{"gen", "entity", "Foo", "name:string"},
		{"migrate", "bogus"},
		{"agents"},
		{"audit"},
	} {
		args := args
		_ = covT_capExit(t, func() {
			covT_capStdout(t, func() { dispatch(args) })
		})
	}
}

func TestValidateScaffoldNameDotDot(t *testing.T) {
	// Contains ".." but no path separator → hits the dedicated branch.
	if err := validateScaffoldName("a..b"); err == nil {
		t.Fatal("expected error for embedded '..'")
	}
}

// ── migrate error branches ────────────────────────────────────────────

func TestMigratorFromArgsErrors(t *testing.T) {
	// No migrations dir.
	covT_chdir(t, t.TempDir())
	if _, _, err := migratorFromArgs(nil); err == nil {
		t.Fatal("missing migrations dir should error")
	}
	// Migrations dir present but no DB URL.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, dir)
	if _, _, err := migratorFromArgs(nil); err == nil {
		t.Fatal("missing db url should error")
	}
}

func TestRunMigrateUpConstructErrorExits(t *testing.T) {
	covT_chdir(t, t.TempDir()) // no migrations dir
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateUp([]string{"--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateDownConstructErrorExits(t *testing.T) {
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateDown([]string{"2", "--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateStatusConstructErrorExits(t *testing.T) {
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateStatus([]string{"--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateForceConstructErrorExits(t *testing.T) {
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateForce([]string{"1", "--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateDownStatusForceSucceed(t *testing.T) {
	dir := covT_migrationsDir(t)
	dbURL := "--db-url=" + filepath.Join(dir, "x.db")
	// Apply, then status (with applied + pending), down, force.
	covT_capStdout(t, func() { runMigrateUp([]string{dbURL}) })
	covT_capStdout(t, func() { runMigrateStatus([]string{dbURL}) })
	covT_capStdout(t, func() { runMigrateDown([]string{"1", dbURL}) })
	covT_capStdout(t, func() { runMigrateForce([]string{"1", dbURL}) })
}

func TestRunMigrateGenerateNoBlueprintExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	// No --from → blueprint required → exit 1.
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateGenerate([]string{"name"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateDiffNoDBExits(t *testing.T) {
	_, bp := covT_writeBlueprint(t)
	t.Setenv("DATABASE_URL", "")
	// No --db-url and no DATABASE_URL → openDiffDB errors → exit 1.
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateDiff([]string{"--from=" + bp}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}
