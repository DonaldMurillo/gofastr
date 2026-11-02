package auth

import (
	"context"
	"testing"
	"time"
)

// TestEntitySessionSweepRunsOutsideItsMutex: sweepExpiredOnCreate held
// sweepMu across the expiry DELETE (the magic-link sweep's 5ade7a35
// shape), so one slow DELETE queued every session Create, every login,
// behind the mutex. The slot is claimed under the lock and the DELETE runs
// outside it.
func TestEntitySessionSweepRunsOutsideItsMutex(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	db.SetMaxOpenConns(1)
	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	// Hold the pool's only connection: the sweep's DELETE waits for it.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.sweepExpiredOnCreate(ctx, time.Now())
	}()
	deadline := time.Now().Add(2 * time.Second)
	for db.Stats().WaitCount == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if db.Stats().WaitCount == 0 {
		t.Fatal("the sweep never reached its DELETE")
	}
	free := store.sweepMu.TryLock()
	if free {
		store.sweepMu.Unlock()
	}
	_ = conn.Close()
	<-done
	if !free {
		t.Fatal("sweepMu is held while the DELETE waits: every Create queues behind the sweep")
	}
}

// TestEntitySessionSweepFailureRetriesLater: a failed sweep left lastSweep
// unadvanced, so a failing DELETE ran again on every Create. It now
// retries after sessionSweepRetry, not on the next Create and not after
// the full interval.
func TestEntitySessionSweepFailureRetriesLater(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()
	insertExpiredSession(t, store, "user-1")
	if _, err := db.Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'sweep refused'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, "user-2", time.Hour); err != nil {
		t.Fatalf("Create with a failing sweep: %v", err)
	}
	if _, err := db.Exec(`DROP TRIGGER no_delete`); err != nil {
		t.Fatal(err)
	}
	staleRows := func() int {
		var n int
		if err := db.QueryRow(store.qTable("SELECT COUNT(*) FROM %s WHERE expires_at < $1"), time.Now()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := store.Create(ctx, "user-3", time.Hour); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 1 {
		t.Fatal("the Create right after a failed sweep swept again: a failing DELETE would run on every Create")
	}
	store.sweepMu.Lock()
	store.lastSweep = store.lastSweep.Add(-sessionSweepRetry)
	store.sweepMu.Unlock()
	if _, err := store.Create(ctx, "user-4", time.Hour); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 0 {
		t.Fatal("a failed sweep waited out the full interval instead of retrying after sessionSweepRetry")
	}
}
