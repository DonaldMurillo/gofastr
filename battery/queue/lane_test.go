package queue

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// newSQLiteDB returns a fresh in-memory SQLite database limited to a single
// connection (so writers serialise). Cleanup is registered via t.Cleanup.
// Used by the lane tests, which need to pass extra DBQueue options beyond the
// worker count that openDBQueue exposes.
func newSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// ============================================================================
// THE starvation test: a dedicated lane worker processes an urgent job while
// every shared worker is saturated by a long-running bulk handler. Determinism
// comes from a "bulk started" signal + a release gate — no sleeps on the
// critical path, so it cannot flake under -race.
// ============================================================================

func TestLaneWorkersBeatBulkSaturation(t *testing.T) {
	db := newSQLiteDB(t)
	q, err := NewDBQueue(db, WithWorkers(1), WithDBLaneWorkers("high", 1))
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	bulkStarted := make(chan struct{})
	releaseBulk := make(chan struct{})
	var urgentDone atomic.Int32

	q.RegisterHandler("bulk", func(_ context.Context, _ Job) error {
		close(bulkStarted)
		<-releaseBulk // occupy the shared worker until released
		return nil
	})
	q.RegisterHandler("urgent", func(_ context.Context, _ Job) error {
		urgentDone.Add(1)
		return nil
	})

	q.Start(ctx)

	// Enqueue one bulk job in the default lane. The single shared worker
	// claims it and blocks, fully saturating the shared pool.
	if err := q.Enqueue(ctx, Job{Type: "bulk"}); err != nil {
		t.Fatalf("enqueue bulk: %v", err)
	}
	<-bulkStarted

	// Now enqueue an urgent job in the reserved "high" lane. Priority alone
	// could not save it — the shared worker is busy and cannot be preempted.
	// Only the dedicated lane worker can claim it.
	if err := q.Enqueue(ctx, Job{Type: "urgent", Lane: "high"}); err != nil {
		t.Fatalf("enqueue urgent: %v", err)
	}
	waitFor(t, func() bool { return urgentDone.Load() == 1 }, 5*time.Second,
		"dedicated lane worker did not process urgent job while shared worker saturated")

	close(releaseBulk)
	q.Close()

	if got := urgentDone.Load(); got != 1 {
		t.Fatalf("expected urgent job processed exactly once, got %d", got)
	}
}

// ============================================================================
// Dedicated lane workers never claim another lane's job: with NO shared
// workers, a default-lane job stays pending while the lane worker drains its
// own lane.
// ============================================================================

func TestDBQueueLaneWorkerOnlyOwnLane(t *testing.T) {
	db := newSQLiteDB(t)
	q, err := NewDBQueue(db, WithWorkers(0), WithDBLaneWorkers("high", 1))
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var ran atomic.Int32
	q.RegisterHandler("work", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})
	q.Start(ctx)

	if err := q.Enqueue(ctx, Job{ID: "def", Type: "work"}); err != nil { // default lane
		t.Fatalf("enqueue default: %v", err)
	}
	if err := q.Enqueue(ctx, Job{ID: "hi", Type: "work", Lane: "high"}); err != nil {
		t.Fatalf("enqueue high: %v", err)
	}

	// The high-lane worker processes its own lane's job.
	waitFor(t, func() bool { return ran.Load() == 1 }, 5*time.Second,
		"lane worker did not process its own lane job")

	// The default-lane job must remain pending: no shared worker exists and
	// the dedicated worker will not claim another lane.
	waitFor(t, func() bool {
		st, _ := q.Stats(ctx)
		return st["pending"] >= 1
	}, 2*time.Second, "default-lane job should remain pending without a shared worker")

	q.Close()
	if got := ran.Load(); got != 1 {
		t.Fatalf("expected exactly 1 run (high lane only), default job must not run, got %d", got)
	}
}

// ============================================================================
// Lane round-trips through Enqueue → ListJobs / Dequeue for DBQueue.
// ============================================================================

func TestDBQueueLaneRoundTrip(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "default", Type: "x"}); err != nil {
		t.Fatalf("enqueue default: %v", err)
	}
	if err := q.Enqueue(ctx, Job{ID: "laned", Type: "x", Lane: "bulk"}); err != nil {
		t.Fatalf("enqueue laned: %v", err)
	}

	// ListJobs hydrates Lane (newest-first: "laned" then "default").
	jobs, err := q.ListJobs(ctx, "", 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	laneByID := map[string]string{jobs[0].ID: jobs[0].Lane, jobs[1].ID: jobs[1].Lane}
	if laneByID["default"] != "" {
		t.Errorf("default job lane: got %q want empty", laneByID["default"])
	}
	if laneByID["laned"] != "bulk" {
		t.Errorf("laned job lane: got %q want bulk", laneByID["laned"])
	}

	// Dequeue hydrates Lane too. Both priority 0 → FIFO by created_at:
	// "default" first, then "laned".
	j1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue 1: %v", err)
	}
	if j1.ID != "default" || j1.Lane != "" {
		t.Fatalf("dequeue 1: id=%q lane=%q want default/empty", j1.ID, j1.Lane)
	}
	j2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue 2: %v", err)
	}
	if j2.ID != "laned" || j2.Lane != "bulk" {
		t.Fatalf("dequeue 2: id=%q lane=%q want laned/bulk", j2.ID, j2.Lane)
	}
}

// ============================================================================
// Lane column is migrated onto a pre-existing table created with the OLD
// schema (no lane column). NewDBQueue must add it and enqueue/dequeue must
// work afterwards.
// ============================================================================

func TestDBQueueLaneColumnMigration(t *testing.T) {
	db := newSQLiteDB(t)
	// Create the table by hand with the schema that shipped BEFORE lane
	// isolation — no lane column at all.
	oldSchema := `CREATE TABLE queue_jobs (
		id            TEXT PRIMARY KEY,
		type          TEXT NOT NULL,
		payload       TEXT,
		priority      INTEGER NOT NULL DEFAULT 0,
		attempts      INTEGER NOT NULL DEFAULT 0,
		max_attempts  INTEGER NOT NULL DEFAULT 3,
		created_at    DATETIME NOT NULL,
		scheduled_at  DATETIME NOT NULL,
		status        TEXT NOT NULL DEFAULT 'pending',
		claimed_at    DATETIME
	)`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("create old schema: %v", err)
	}

	// NewDBQueue runs ensureTable, which must migrate the lane column on.
	q, err := NewDBQueue(db, WithWorkers(0))
	if err != nil {
		t.Fatalf("new db queue (migrate): %v", err)
	}
	ctx := context.Background()

	// A lane-tagged job must enqueue + dequeue + round-trip the lane value,
	// proving the column exists and is wired into the claim/scan paths.
	if err := q.Enqueue(ctx, Job{Type: "x", Lane: "high"}); err != nil {
		t.Fatalf("enqueue after migrate: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue after migrate: %v", err)
	}
	if job.Lane != "high" {
		t.Fatalf("lane round-trip after migration: got %q want high", job.Lane)
	}

	// Verify the column is actually present in the schema (not just tolerated).
	var colName string
	err = db.QueryRow("SELECT name FROM pragma_table_info('queue_jobs') WHERE name='lane'").Scan(&colName)
	if err != nil {
		t.Fatalf("lane column missing after migration: %v", err)
	}
}

// ============================================================================
// MemoryQueue lane reservation: a dedicated lane worker processes an urgent
// job while the shared worker is saturated by a blocking bulk handler.
// ============================================================================

func TestMemoryLaneWorkerIsolates(t *testing.T) {
	q := NewMemoryQueue(1, WithLaneWorkers("high", 1))

	bulkStarted := make(chan struct{})
	releaseBulk := make(chan struct{})
	var urgentDone atomic.Int32

	q.RegisterHandler("bulk", func(_ context.Context, _ Job) error {
		close(bulkStarted)
		<-releaseBulk
		return nil
	})
	q.RegisterHandler("urgent", func(_ context.Context, _ Job) error {
		urgentDone.Add(1)
		return nil
	})

	q.Start()
	t.Cleanup(func() {
		close(releaseBulk)
		q.Close()
	})

	if err := q.Enqueue(context.Background(), Job{Type: "bulk"}); err != nil {
		t.Fatalf("enqueue bulk: %v", err)
	}
	<-bulkStarted // shared worker is saturated

	if err := q.Enqueue(context.Background(), Job{Type: "urgent", Lane: "high"}); err != nil {
		t.Fatalf("enqueue urgent: %v", err)
	}
	waitFor(t, func() bool { return urgentDone.Load() == 1 }, 5*time.Second,
		"dedicated lane worker did not process urgent job while shared worker saturated")
}
