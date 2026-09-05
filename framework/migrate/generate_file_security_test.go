package migrate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGenerateMigrationFileRefusesPlantedNextFile pins the scan-to-write
// window in migration-file generation, found by the 2026-09-04 red-probe
// round; fixed by creating the file through an *os.Root over
// MigrationsDir with O_CREATE|O_EXCL, so a symlink planted at the chosen
// name is kernel-refused and a pre-existing entry is never clobbered.
//
// Family: F3 path canonicalization at filesystem sinks (lexical Join
// under a caller-supplied directory, followed at write time).
// Property: a write under MigrationsDir must land inside MigrationsDir —
// containment may not rest on filepath.Join plus the version scan,
// because the directory can change between the scan and the write.
// Surfaces: framework/migrate/generate_file.go:GenerateMigrationFile
// (the create at the chosen NNNN_name.sql path; the rootwrite analyzer
// polices this shape). The internal afterVersionScan seam drives the
// interleave deterministically; it is nil in production (tests here
// must not run in parallel while it is installed).

func TestGenerateMigrationFileRefusesPlantedNextFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs developer mode on windows; shape covered by review")
	}

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrations, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The version scan has committed to 0001_initial.sql (empty dir);
	// the plant lands exactly on that name, invisible to the numbering.
	afterVersionScan = func(d, filename string) {
		if d != migrations || filename != "0001_initial.sql" {
			return
		}
		if err := os.Symlink(sentinel, filepath.Join(d, filename)); err != nil {
			t.Errorf("plant: %v", err)
		}
	}
	t.Cleanup(func() { afterVersionScan = nil })

	opts := MigrationFileOptions{
		MigrationsDir: migrations,
		SnapshotPath:  filepath.Join(migrations, "schema.snapshot.json"),
		Dialect:       DialectSQLite,
	}
	path, err := GenerateMigrationFile(Plan{Registry: blogReg(nil)}, "initial", opts)
	if err == nil {
		t.Errorf("SECURITY: [migrate-plant-write] GenerateMigrationFile reported success (%q) through a symlink planted at the next versioned filename after the version scan: the migration SQL was written through the link, outside %s.", path, migrations)
	}
	got, rerr := os.ReadFile(sentinel)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != "SENTINEL" {
		t.Errorf("SECURITY: [migrate-plant-write] the outside sentinel was overwritten through the planted symlink: %q", string(got))
	}
}
