package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openDBQueue returns a fresh in-memory SQLite + DBQueue pair. Cleanup is
// registered via t.Cleanup. Tests run against SQLite for speed; the
// framework-level e2e exercises the Postgres path.
func openDBQueue(t *testing.T, workers int) (*sql.DB, *DBQueue) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// SQLite ":memory:" with the default pool size races against itself —
	// limit to 1 conn so writers serialise on the single in-memory page.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	q, err := NewDBQueue(db, WithWorkers(workers))
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	return db, q
}

// ============================================================================
// Smoke: Enqueue → Dequeue returns the job we wrote.
// ============================================================================

func TestDBQueue_EnqueueDequeueRoundTrip(t *testing.T) {
	_, q := openDBQueue(t, 0)

	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{
		Type:    "send-email",
		Payload: json.RawMessage(`{"to":"a@b.com"}`),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if job.Type != "send-email" {
		t.Fatalf("type: got %q", job.Type)
	}
	if string(job.Payload) != `{"to":"a@b.com"}` {
		t.Fatalf("payload: got %s", job.Payload)
	}
	if job.Attempts != 1 {
		t.Fatalf("expected attempts=1 after claim, got %d", job.Attempts)
	}
}

// ============================================================================
// Dequeue is empty after we drain the only job.
// ============================================================================

func TestDBQueue_DequeueEmpty(t *testing.T) {
	_, q := openDBQueue(t, 0)

	ctx := context.Background()
	q.Enqueue(ctx, Job{Type: "x"})
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("first dequeue: %v", err)
	}
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("expected ErrNoJob, got %v", err)
	}
}

// ============================================================================
// Type filter: only the named type is returned even when others are pending.
// ============================================================================

func TestDBQueue_DequeueTypeFilter(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()

	q.Enqueue(ctx, Job{Type: "alpha"})
	q.Enqueue(ctx, Job{Type: "beta"})
	q.Enqueue(ctx, Job{Type: "gamma"})

	job, err := q.Dequeue(ctx, "beta")
	if err != nil {
		t.Fatalf("dequeue beta: %v", err)
	}
	if job.Type != "beta" {
		t.Fatalf("type: got %q want beta", job.Type)
	}
}

// ============================================================================
// Priority + FIFO ordering: higher priority first, then created_at ASC.
// ============================================================================

func TestDBQueue_PriorityOrdering(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()

	now := time.Now().UTC()
	// Two jobs at default priority, then a high-priority one inserted last.
	q.Enqueue(ctx, Job{ID: "first", Type: "x", CreatedAt: now.Add(-2 * time.Second)})
	q.Enqueue(ctx, Job{ID: "second", Type: "x", CreatedAt: now.Add(-1 * time.Second)})
	q.Enqueue(ctx, Job{ID: "boost", Type: "x", Priority: 10, CreatedAt: now})

	got := []string{}
	for i := 0; i < 3; i++ {
		j, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("dequeue %d: %v", i, err)
		}
		got = append(got, j.ID)
	}
	want := []string{"boost", "first", "second"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order: got %v want %v", got, want)
		}
	}
}

// ============================================================================
// Scheduled jobs in the future are not yet eligible.
// ============================================================================

func TestDBQueue_ScheduledFutureNotEligible(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()

	q.Enqueue(ctx, Job{
		Type:        "later",
		ScheduledAt: time.Now().Add(10 * time.Minute),
	})
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("expected ErrNoJob for future job, got %v", err)
	}
}

// ============================================================================
// Ack permanently removes the row.
// ============================================================================

func TestDBQueue_AckRemovesRow(t *testing.T) {
	db, q := openDBQueue(t, 0)
	ctx := context.Background()

	q.Enqueue(ctx, Job{Type: "x"})
	job, _ := q.Dequeue(ctx)
	if err := q.Ack(ctx, job.ID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM queue_jobs WHERE id = ?", job.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected row deleted, count=%d", count)
	}
}

// ============================================================================
// Nack returns the job to 'pending' so a retry can pick it up.
// ============================================================================

func TestDBQueue_NackRequeuesWhenRetriesLeft(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()

	q.Enqueue(ctx, Job{Type: "x", MaxAttempts: 3})
	job1, _ := q.Dequeue(ctx)
	if err := q.Nack(ctx, job1.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}
	job2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("re-dequeue after nack: %v", err)
	}
	if job2.ID != job1.ID {
		t.Fatalf("expected same id after nack, got %s vs %s", job2.ID, job1.ID)
	}
	if job2.Attempts != 2 {
		t.Fatalf("expected attempts=2 after re-claim, got %d", job2.Attempts)
	}
}

// ============================================================================
// Nack marks the job 'failed' once max_attempts is reached.
// ============================================================================

func TestDBQueue_NackMovesToFailedAtMaxAttempts(t *testing.T) {
	db, q := openDBQueue(t, 0)
	ctx := context.Background()

	q.Enqueue(ctx, Job{Type: "x", MaxAttempts: 1})
	job, _ := q.Dequeue(ctx) // attempts now = 1 = max
	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}
	var status string
	db.QueryRow("SELECT status FROM queue_jobs WHERE id = ?", job.ID).Scan(&status)
	if status != "failed" {
		t.Fatalf("expected status=failed, got %q", status)
	}
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("failed jobs should not be returned by Dequeue")
	}
}

// ============================================================================
// End-to-end: Start workers, enqueue jobs, watch them complete via Ack.
// ============================================================================

func TestDBQueue_WorkerLoopProcessesJobs(t *testing.T) {
	db, q := openDBQueue(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var done atomic.Int32
	q.RegisterHandler("ping", func(_ context.Context, _ Job) error {
		done.Add(1)
		return nil
	})

	q.Start(ctx)

	for i := 0; i < 5; i++ {
		if err := q.Enqueue(ctx, Job{Type: "ping"}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for done.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	q.Close()

	if got := done.Load(); got != 5 {
		t.Fatalf("expected 5 completions, got %d", got)
	}
	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM queue_jobs").Scan(&remaining)
	if remaining != 0 {
		t.Fatalf("expected 0 rows after ack, got %d", remaining)
	}
}

// ============================================================================
// Worker loop nacks on handler error and retries up to MaxAttempts.
// ============================================================================

func TestDBQueue_WorkerRetriesOnHandlerError(t *testing.T) {
	db, q := openDBQueue(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var calls atomic.Int32
	q.RegisterHandler("flaky", func(_ context.Context, _ Job) error {
		calls.Add(1)
		return errors.New("transient")
	})
	q.Start(ctx)

	q.Enqueue(ctx, Job{Type: "flaky", MaxAttempts: 3})

	// Three retries should happen, then the job lands in 'failed'.
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	q.Close()

	if got := calls.Load(); got != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", got)
	}
	var status string
	db.QueryRow("SELECT status FROM queue_jobs LIMIT 1").Scan(&status)
	if status != "failed" {
		t.Fatalf("expected status=failed after exhausting retries, got %q", status)
	}
}

// ============================================================================
// Close is idempotent and unblocks before workers leak.
// ============================================================================

func TestDBQueue_CloseIdempotent(t *testing.T) {
	_, q := openDBQueue(t, 1)
	q.Start(context.Background())
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("close 2nd: %v", err)
	}
}
