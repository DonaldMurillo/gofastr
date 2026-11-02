package queue

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A shutdown that lands after a worker claimed a job but before it started
// the handler ran the handler anyway, on a context already cancelled: a
// cooperative handler returned at once, the job was nacked, and its
// attempt was spent on a run that never happened (with MaxAttempts 1, it
// was dead-lettered). The worker now hands such a job back unstarted with
// its attempt refunded, as the outbox relay does with the rest of a batch.
func TestShutdownBeforeStartRefundsClaim(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		db, q := openDBQueueOpts(t, WithWorkers(0))
		assertShutdownBeforeStartRefunds(t, db, q)
	})
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.DB(t)
		q, err := NewDBQueue(db, WithWorkers(0))
		if err != nil {
			t.Fatal(err)
		}
		assertShutdownBeforeStartRefunds(t, db, q)
	})
}

func assertShutdownBeforeStartRefunds(t *testing.T, db *sql.DB, q *DBQueue) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ran atomic.Int32
	q.RegisterHandler("t", func(ctx context.Context, _ Job) error {
		ran.Add(1)
		return ctx.Err()
	})
	var calls atomic.Int32
	q.SetGate(func(string) bool {
		// The second call is the re-check after the claim: the shutdown
		// lands there, and the gate itself still allows the job.
		if calls.Add(1) == 2 {
			cancel()
		}
		return true
	})
	if err := q.Enqueue(context.Background(), Job{ID: "j", Type: "t", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	q.workerLoop(ctx, "")

	var status string
	var attempts int
	if err := db.QueryRow("SELECT status, attempts FROM "+q.qt()+" WHERE id = 'j'").Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if n := ran.Load(); n != 0 || status != "pending" || attempts != 0 {
		t.Fatalf("job claimed as the shutdown landed: handler ran %d times, %q with %d attempts; want unstarted, pending with 0", n, status, attempts)
	}
}
