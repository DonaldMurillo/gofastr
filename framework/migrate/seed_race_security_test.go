package migrate

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Pins the SQLite seed double-run, found by the 2026-09-04 red-probe round;
// fixed in seed.go by giving RunSeeds's SQLite arm a real twin of the Postgres
// advisory lock: a process-level mutex plus a leased lock row
// (_gofastr_seed_lock, renewed by heartbeat) held across read → run → record.
// Family: F16 check-then-act across processes (F7 duplicate side effect with
// no idempotency)
// Property: RunSeeds's documented contract — "an entity's Seed run ONCE
// globally: whichever replica wins the lock runs the body and records the row;
// the others wait, then short-circuit on the ledger" — must hold on every
// dialect the runner supports; the ledger read → run → record sequence is not
// a mutual exclusion by itself.
// Surfaces: framework/migrate/seed.go::RunSeeds (SQLite arm takes
// sqliteSeedMu then the leased lock row before runSeedsBody),
// framework/migrate/seed.go::acquireSQLiteSeedLease (atomic upsert that
// steals only an expired lease; heartbeat renewal; DELETE release),
// framework/migrate/seed.go::runSeedsBody (read-ledger → run-body →
// record-ledger inside the lock), framework/migrate/seed.go::recordSeeded
// (ON CONFLICT DO NOTHING dedupes the ledger ROW, not the Seed execution —
// its own comment says so).
// Contrast: framework/migrate/seed_lock_test.go::
// TestRunSeeds_AdvisoryLockSerializesAcrossReplicas pins the ONCE property on
// Postgres; migrate.go::addMissingColumns explicitly contemplates concurrent
// multi-process SQLite boots, so multi-process SQLite is a supported
// deployment shape in this very package.

// secSeedReg is a one-entity registry whose entity carries the Seed fn.
type secSeedReg map[string]*entity.Entity

func (r secSeedReg) All() map[string]*entity.Entity { return r }

func (r secSeedReg) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r))
	for _, e := range r {
		out = append(out, e)
	}
	return out
}

func (r secSeedReg) Get(name string) (*entity.Entity, error) {
	if e, ok := r[name]; ok {
		return e, nil
	}
	return nil, errSecSeedNotFound
}

type secSeedNotFound struct{}

var errSecSeedNotFound = secSeedNotFound{}

func (secSeedNotFound) Error() string { return "entity not found" }

// TestRunSeedsSqliteSeedRunsOnce: two concurrent RunSeeds against one SQLite
// database must execute the Seed body exactly once, exactly as the pinned
// Postgres twin does under the advisory lock.
func TestRunSeedsSqliteSeedRunsOnce(t *testing.T) {
	// A FILE, not ":memory:": modernc gives every pooled connection to
	// ":memory:" its own empty database, so the two RunSeeds calls would
	// race against SEPARATE ledgers no lock could unite. The property under
	// test is two replicas against ONE database (sqlite_ownerfk_test.go
	// uses the same file-backed shape for the same reason).
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "seeds.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var runs int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	seed := func(ctx context.Context, _ *sql.DB) error {
		atomic.AddInt32(&runs, 1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
		}
		return nil
	}
	// Both replicas see the other inside the body: open the gate.
	go func() {
		<-entered
		<-entered
		close(release)
	}()

	seeded := rawEnt("seeded", "seeded", nil, nil, "id")
	seeded.Config.Seed = seed
	reg := secSeedReg{"seeded": seeded}

	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			errCh <- RunSeeds(context.Background(), db, reg)
		}()
	}
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("RunSeeds: %v", err)
		}
	}

	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("SECURITY: [seed] the Seed body ran %d times across two concurrent RunSeeds on SQLite "+
			"(want exactly 1, as the Postgres advisory-lock twin pins): the ledger read → run → record "+
			"sequence must run under a real lock on SQLite, not bare", got)
	}
}
