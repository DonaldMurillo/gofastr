package queue

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openDBQueueOpts is like openDBQueue but lets a test supply extra options
// (e.g. WithBackoff) alongside the default worker count.
func openDBQueueOpts(t *testing.T, opts ...DBQueueOption) (*sql.DB, *DBQueue) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	q, err := NewDBQueue(db, opts...)
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	return db, q
}

// q-1: With WithBackoff, a Nack with retries remaining pushes scheduled_at into
// the future so the failing job is not immediately eligible again.
func TestDBQueue_NackBackoffDelaysRetry(t *testing.T) {
	db, q := openDBQueueOpts(t, WithWorkers(0), WithBackoff(time.Minute, time.Hour))
	ctx := context.Background()

	q.Enqueue(ctx, Job{Type: "x", MaxAttempts: 3})
	job, _ := q.Dequeue(ctx) // attempts now = 1
	before := time.Now().UTC()
	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}

	// scheduled_at must be advanced beyond now (base*2^(attempts-1) ≈ 1m).
	var sched time.Time
	if err := db.QueryRow("SELECT scheduled_at FROM queue_jobs WHERE id = ?", job.ID).Scan(&sched); err != nil {
		t.Fatalf("scan scheduled_at: %v", err)
	}
	if !sched.After(before.Add(30 * time.Second)) {
		t.Fatalf("expected scheduled_at pushed >= ~1m into the future, got %v (before=%v)", sched.UTC(), before)
	}

	// The job must NOT be immediately eligible after the backoff Nack.
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("expected ErrNoJob during backoff window, got %v", err)
	}
}

// q-1: Backoff growth is capped at the configured maximum.
func TestDBQueue_NackBackoffCapped(t *testing.T) {
	db, q := openDBQueueOpts(t, WithWorkers(0), WithBackoff(time.Second, 2*time.Second))
	ctx := context.Background()

	// High attempts so base*2^attempts would blow far past the 2s cap.
	q.Enqueue(ctx, Job{Type: "x", Attempts: 20, MaxAttempts: 100})
	job, _ := q.Dequeue(ctx) // attempts now = 21
	before := time.Now().UTC()
	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}
	var sched time.Time
	if err := db.QueryRow("SELECT scheduled_at FROM queue_jobs WHERE id = ?", job.ID).Scan(&sched); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sched.UTC().After(before.Add(10 * time.Second)) {
		t.Fatalf("backoff not capped: scheduled_at %v is far beyond the 2s cap (before=%v)", sched.UTC(), before)
	}
}

// q-1: Without WithBackoff, the default behaviour is preserved — a Nack with
// retries remaining makes the job immediately eligible again.
func TestDBQueue_NackNoBackoffByDefault(t *testing.T) {
	_, q := openDBQueueOpts(t, WithWorkers(0))
	ctx := context.Background()

	q.Enqueue(ctx, Job{Type: "x", MaxAttempts: 3})
	job1, _ := q.Dequeue(ctx)
	if err := q.Nack(ctx, job1.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}
	job2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("re-dequeue after nack (no backoff): %v", err)
	}
	if job2.ID != job1.ID {
		t.Fatalf("expected same id, got %s vs %s", job2.ID, job1.ID)
	}
}

// q-2: MemoryQueue.Nack must not silently drop the job. It re-enqueues a job
// with retries remaining so it is processed again.
func TestMemoryQueue_NackReEnqueues(t *testing.T) {
	q := NewMemoryQueue(0) // manual consumption, no workers
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "t", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}

	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}

	// The job must be back on the queue, not dropped.
	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("expected job re-enqueued after nack, got %v", err)
	}
	if got.ID != "j1" {
		t.Fatalf("expected re-enqueued job j1, got %q", got.ID)
	}
	if got.Attempts != 1 {
		t.Fatalf("expected attempts incremented to 1, got %d", got.Attempts)
	}
}

// q-2: Nacking a job that has exhausted its attempts does not re-enqueue it.
func TestMemoryQueue_NackExhaustedDropsJob(t *testing.T) {
	q := NewMemoryQueue(0)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "t", Attempts: 2, MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	// attempts will become 3 == max on nack → not re-enqueued.
	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("nack: %v", err)
	}
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("expected ErrNoJob (job exhausted, not re-enqueued), got %v", err)
	}
}

// q-3: RegisterHandler and SetLeaseTimeout must not race with a running worker
// loop. Run under -race: concurrent registration + the worker's map/lease reads
// must be synchronised.
func TestDBQueue_RegisterHandlerNoRaceWithWorker(t *testing.T) {
	_, q := openDBQueueOpts(t, WithWorkers(2))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.RegisterHandler("seed", func(_ context.Context, _ Job) error { return nil })
	q.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				q.RegisterHandler("t", func(_ context.Context, _ Job) error { return nil })
				q.SetLeaseTimeout(time.Duration(j+1) * time.Second)
				_ = q.Enqueue(ctx, Job{Type: "t"})
			}
		}(i)
	}
	wg.Wait()
	q.Close()
}
