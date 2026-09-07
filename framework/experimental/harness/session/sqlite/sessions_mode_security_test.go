package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Pins: the harness session event DB (plaintext transcripts) is
// Property: the harness session event DB (plaintext transcripts) is
// owner-only on disk — 0600 file, matching the package's own words:
// export.go:55-57 ("the sibling sqlite store already writes this data
// owner-only (0700 dir / 0600 files)") and encrypted.go:43 ("The
// unencrypted path is created with mode 0600").
// Surfaces: sqlite.go::Open :54-58 (sql.Open on a driver that creates
// the db 0644-under-umask; no seed, no chmod) + retention.go::
// openCurrentLocked :122-125 (same Open under MonthlyRollover, so the
// monthly sessions-YYYYMM.db archives inherit the hole).
// Finding: Open hands the path straight to the SQLite driver, which
// creates the database AND its -wal sidecar with the default
// 0666&~umask mode (0644 under umask 022 — probe-verified). The event
// log carries full plaintext session transcripts; a 0644 file plus a
// 0644 -wal makes them group/world-readable on a multi-user box. The
// correct seed twin sits ~40 lines away in the same package:
// OpenCostLedger (retention.go:163-169) O_CREATE-seeds the file 0600
// precisely because "the WAL sidecars inherit the main db's mode on
// creation". Open never got that cutover.
// Fix direction: mirror OpenCostLedger — seed <path> with
// O_CREATE|O_RDWR 0600 before sql.Open (WAL sidecars then inherit
// 0600), same for openCurrentLocked via the shared Open.
func TestSessionsDBRedOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	dir := t.TempDir()
	// Umask guard: the fresh-create finding manifests as group/world
	// bits surviving the process umask. If THIS environment's umask
	// already masks them, the premise cannot reproduce here.
	probe := filepath.Join(dir, "umask-probe")
	if err := os.WriteFile(probe, nil, 0o666); err != nil {
		t.Fatalf("setup broken: umask probe: %v", err)
	}
	pi, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("setup broken: stat umask probe: %v", err)
	}
	if pi.Mode().Perm()&0o077 == 0 {
		t.Skip("environment umask masks group/world bits; fresh-create mode cannot manifest")
	}

	path := filepath.Join(dir, "s.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("setup broken: Open: %v", err)
	}
	defer s.Close()

	// One write so the WAL sidecar exists alongside the main db.
	env, err := control.EncodeEvent(1, control.TextDelta{Text: "session transcript body"}, ids.NewSessionID(), ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatalf("setup broken: EncodeEvent: %v", err)
	}
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatalf("setup broken: AppendEvent: %v", err)
	}

	for _, p := range []string{path, path + "-wal"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("setup broken: stat %s: %v (write did not materialize the sidecar)", p, err)
		}
	}

	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("setup broken: stat db: %v", err)
	} else if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [sessions-db-mode] session event DB %s is %v — Open never seeds 0600 (sqlite.go::Open :54-58 hands the path to the driver, which creates it 0666&~umask), so plaintext transcripts are group/world-readable; the sibling OpenCostLedger 40 lines away (retention.go:163-169) seeds exactly this way and its own comment claims the event store 'already writes this data owner-only (0700 dir / 0600 files)'", path, fi.Mode().Perm())
	}

	if fi, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("setup broken: stat wal: %v", err)
	} else if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [sessions-db-mode] session event WAL sidecar %s is %v — it inherits the unseeded db's mode on creation (retention.go OpenCostLedger comment states the inheritance), so the un-checkpointed plaintext tail is group/world-readable too", path+"-wal", fi.Mode().Perm())
	}

	// Control leg: pin the family grammar via the correct twin in the
	// same package. Passes today; if it ever fails, the premise of
	// this red test (0600 is the contract) collapsed.
	ledgerPath := filepath.Join(dir, "cost.db")
	ledger, err := OpenCostLedger(ledgerPath)
	if err != nil {
		t.Fatalf("setup broken: OpenCostLedger: %v", err)
	}
	defer ledger.Close()
	lfi, err := os.Stat(ledgerPath)
	if err != nil {
		t.Fatalf("setup broken: stat cost ledger: %v", err)
	}
	if lfi.Mode().Perm() != 0o600 {
		t.Fatalf("setup broken: cost ledger control leg is %v, want 0600 — the pinned twin regressed", lfi.Mode().Perm())
	}
}
