package queue

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A DBQueue worker that claimed a job whose type had no handler acked it:
// the row was deleted (or marked done), so the job vanished with no
// trace, while MemoryQueue keeps such a job as terminally failed where
// ListJobs shows it and Replay re-runs it. DBQueue now dead-letters it
// the same way. The claim filters on registered types, so the branch is
// reached when the handler is gone between the claim and the lookup;
// the test removes it in that gap.
func TestNoHandlerJobIsDeadLettered(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		db, q := openDBQueueOpts(t, WithWorkers(0))
		assertNoHandlerDeadLetters(t, db, q)
	})
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.DB(t)
		q, err := NewDBQueue(db, WithWorkers(0))
		if err != nil {
			t.Fatal(err)
		}
		assertNoHandlerDeadLetters(t, db, q)
	})
}

func assertNoHandlerDeadLetters(t *testing.T, db *sql.DB, q *DBQueue) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.RegisterHandler("t", func(context.Context, Job) error { return nil })
	var claimed atomic.Int32
	q.afterClaim = func(Job) {
		claimed.Add(1)
		q.mu.Lock()
		delete(q.handlers, "t")
		q.mu.Unlock()
		// No type is left to claim; stop once this job is settled.
		cancel()
	}
	if err := q.Enqueue(context.Background(), Job{ID: "j", Type: "t", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	q.workerLoop(ctx, "")
	if claimed.Load() != 1 {
		t.Fatalf("the worker claimed the job %d times, want once", claimed.Load())
	}

	var status, lastError string
	if err := db.QueryRow("SELECT status, COALESCE(last_error, '') FROM "+q.qt()+" WHERE id = 'j'").Scan(&status, &lastError); err != nil {
		t.Fatalf("the job is gone (%v); want it kept as failed", err)
	}
	if status != "failed" || !strings.Contains(lastError, "no handler") {
		t.Fatalf("job = %q (%q), want failed naming the missing handler", status, lastError)
	}
	jobs, err := q.ListJobs(context.Background(), "failed", 10, 0)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "j" {
		t.Fatalf("ListJobs(failed) = %v, %v; want the job", jobs, err)
	}
	if err := q.Replay(context.Background(), "j"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if err := db.QueryRow("SELECT status FROM " + q.qt() + " WHERE id = 'j'").Scan(&status); err != nil || status != "pending" {
		t.Fatalf("after Replay: %q, %v; want pending", status, err)
	}
}
