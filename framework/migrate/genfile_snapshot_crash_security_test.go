package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Property: GenerateMigrationFile must be safe to re-run after its snapshot
// update failed: the delta already committed as migration N must not be
// emitted a second time as migration N+1, because at apply time the duplicate
// fails mid-deploy (its CREATE TABLE hits the table N just made) and marks
// the database dirty.
// Surfaces: framework/migrate/generate_file.go::GenerateMigrationFile (write
// file first, SaveSnapshot second, the crash window between them) and
// framework/migrate/snapshot.go::LoadSnapshot (a missing/corrupt snapshot
// reads as empty, so the stale state re-generates the same delta).
// Guard history: call 1 wrote 0001_initial.sql and failed at SaveSnapshot
// (any write failure or crash in that window leaves the same state). Call 2,
// same options, succeeded and returned 0002_initial.sql whose Up section was
// byte-identical to 0001's — two migrations carrying one delta, the second of
// which could never apply cleanly. The re-run now reconciles: it recognizes
// that the last committed migration already carries this delta (Up and Down
// sections match by content), skips minting, and repairs the snapshot.
func TestGenFileRerunAfterSnapshotFail(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	snapDir := filepath.Join(dir, "snap")
	snapPath := filepath.Join(snapDir, "schema.snapshot.json")
	// Only the migrations directory exists. The snapshot directory is
	// deliberately absent: LoadSnapshot reads the missing file as an
	// empty snapshot, while SaveSnapshot's CreateTemp into the missing
	// directory fails — the committed-file / stale-snapshot crash state.
	// An absent directory blocks root as surely as anyone: there are no
	// permission bits for root to bypass.
	if err := os.MkdirAll(migrations, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := func() {
		if err := os.MkdirAll(snapDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	opts := MigrationFileOptions{
		MigrationsDir: migrations,
		SnapshotPath:  snapPath,
		Dialect:       DialectSQLite,
	}
	reg := blogReg(nil)

	// The obstruction is already in place: the snapshot directory does
	// not exist, so the SaveSnapshot below fails while LoadSnapshot
	// above read the pre-state as empty.
	path1, err := GenerateMigrationFile(Plan{Registry: reg}, "initial", opts)
	if err == nil {
		t.Fatalf("call 1 unexpectedly succeeded (path %q): the obstructed snapshot write must fail", path1)
	}
	if !strings.Contains(err.Error(), "snapshot update failed") {
		t.Fatalf("call 1 failed at the wrong step: %v", err)
	}
	first, rerr := os.ReadFile(filepath.Join(migrations, "0001_initial.sql"))
	if rerr != nil {
		t.Fatalf("call 1 did not commit the migration file: %v", rerr)
	}

	// The operator clears the obstruction (or the process simply didn't
	// crash the second time) and re-runs the exact same command.
	restore()
	path2, err := GenerateMigrationFile(Plan{Registry: reg}, "initial", opts)
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}

	if path2 != "" {
		second, rerr := os.ReadFile(path2)
		if rerr != nil {
			t.Fatal(rerr)
		}
		upFirst, _, _, err := splitMigrationSections(string(first))
		upSecond, _, _, err2 := splitMigrationSections(string(second))
		if err != nil || err2 != nil {
			t.Fatalf("split sections: %v / %v", err, err2)
		}
		if strings.TrimSpace(upFirst) != "" && upFirst == upSecond {
			t.Errorf("SECURITY: [genfile-crash-dupe] re-run after the failed snapshot update emitted %s with an Up section byte-identical to the already-committed 0001_initial.sql — the crash window between the file write and SaveSnapshot duplicates the delta as a new version, which fails at apply time (table already exists) and dirties the database; the re-run must reconcile (skip + repair the snapshot), not mint N+1",
				filepath.Base(path2))
		}
	}
}

// splitMigrationSections extracts the Up and Down bodies from a rendered
// `-- +migrate` file so the duplicate assertion compares the DDL rather than
// the version-stamped header. Deliberately independent of the production
// parser (parseMigrationSections) so a parser bug cannot mask a reconcile
// bug.
func splitMigrationSections(content string) (up, down string, version string, err error) {
	rest := content
	if i := strings.Index(content, "-- +migrate Up\n"); i >= 0 {
		rest = content[i+len("-- +migrate Up\n"):]
	} else {
		return "", "", "", errBadSection("no Up directive")
	}
	if j := strings.Index(rest, "-- +migrate Down\n"); j >= 0 {
		return rest[:j], rest[j+len("-- +migrate Down\n"):], "", nil
	}
	return rest, "", "", nil
}

type errBadSection string

func (e errBadSection) Error() string { return string(e) }
