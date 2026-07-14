package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitPostgresWithEntities(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"pg", "--db=postgres"}) })
	main, err := os.ReadFile(filepath.Join(dir, "pg", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "postgres") && !strings.Contains(string(main), "pq") && !strings.Contains(string(main), "pgx") {
		t.Fatalf("postgres main.go should reference a pg driver:\n%s", main[:200])
	}
}

// The write* helpers osExit when their target directory does not exist.
// Pointing name at a missing path exercises the write-failure branch.
func covT_missingDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "does", "not", "exist")
}

func TestWriteMainGoWriteFailureExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { writeMainGo(covT_missingDir(t), "m", false, "sqlite", "file:x.db") })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestWriteIsolationConfigWriteFailureExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { writeIsolationConfig(covT_missingDir(t), "sqlite") })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestWriteHomeScreenWriteFailureExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { writeHomeScreen(covT_missingDir(t), true) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestWriteDesignMDWriteFailure(t *testing.T) {
	if err := writeDesignMD(covT_missingDir(t)); err == nil {
		t.Fatal("writeDesignMD should fail when the target directory is missing")
	}
}

func TestWriteEntitiesGoWriteFailureExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { writeEntitiesGo(covT_missingDir(t)) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestWriteHomeScreenNoEntityVsEntity(t *testing.T) {
	d1 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d1, "screens"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Both branches of the noEntity hint.
	covT_capStdout(t, func() { writeHomeScreen(d1, true) })
	d2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d2, "screens"), 0o755); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { writeHomeScreen(d2, false) })
}
