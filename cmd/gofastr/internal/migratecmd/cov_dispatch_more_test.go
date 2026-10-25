package migratecmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── migrate error branches ────────────────────────────────────────────

func TestMigratorFromArgsErrors(t *testing.T) {
	// No migrations dir.
	clitest.CovT_chdir(t, t.TempDir())
	if _, _, err := migratorFromArgs(nil); err == nil {
		t.Fatal("missing migrations dir should error")
	}
	// Migrations dir present but no DB URL. The environment has to be
	// cleared, not assumed: migratorFromArgs falls back to DATABASE_URL,
	// and anyone working on a Postgres app is likely to have it exported,
	// which turns "missing db url should error" into a failure that
	// depends on whose shell ran the suite.
	t.Setenv("DATABASE_URL", "")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, dir)
	if _, _, err := migratorFromArgs(nil); err == nil {
		t.Fatal("missing db url should error")
	}
}

func TestRunMigrateUpConstructErrorExits(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir()) // no migrations dir
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runMigrateUp([]string{"--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateDownConstructErrorExits(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runMigrateDown([]string{"2", "--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateStatusConstructErrorExits(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runMigrateStatus([]string{"--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateForceConstructErrorExits(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runMigrateForce([]string{"1", "--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateDownStatusForceSucceed(t *testing.T) {
	dir := covT_migrationsDir(t)
	dbURL := "--db-url=" + filepath.Join(dir, "x.db")
	// Apply, then status (with applied + pending), down, force.
	clitest.CovT_capStdout(t, func() { runMigrateUp([]string{dbURL}) })
	clitest.CovT_capStdout(t, func() { runMigrateStatus([]string{dbURL}) })
	clitest.CovT_capStdout(t, func() { runMigrateDown([]string{"1", dbURL}) })
	clitest.CovT_capStdout(t, func() { runMigrateForce([]string{"1", dbURL}) })
}

func TestRunMigrateGenerateNoAppSpecExits(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	// No --from → app spec required → exit 1.
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runMigrateGenerate([]string{"name"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}
