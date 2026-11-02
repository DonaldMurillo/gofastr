package queue

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A worker that claims a job and then finds its type gated hands the claim
// back with its attempt refunded. The release ran on the worker's context,
// so a shutdown landing between the claim and the release made the write
// fail: the row stayed 'claimed' with the attempt spent, and on its last
// attempt the dead-letter sweep failed a job that never ran once the lease
// expired. The release now settles on a detached context, like Ack and Nack.
func TestGateReleaseSurvivesShutdown(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		db, q := openDBQueueOpts(t, WithWorkers(0))
		assertGateReleaseSurvivesShutdown(t, db, q)
	})
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.DB(t)
		q, err := NewDBQueue(db, WithWorkers(0))
		if err != nil {
			t.Fatal(err)
		}
		assertGateReleaseSurvivesShutdown(t, db, q)
	})
}

func assertGateReleaseSurvivesShutdown(t *testing.T, db *sql.DB, q *DBQueue) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	q.RegisterHandler("t", func(context.Context, Job) error {
		t.Error("a gated job ran")
		return nil
	})
	q.SetGate(func(string) bool {
		// The first call is the pre-claim filter; the second is the
		// re-check after the claim, where the shutdown lands.
		if calls.Add(1) == 1 {
			return true
		}
		cancel()
		return false
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
	if status != "pending" || attempts != 0 {
		t.Fatalf("gated job after a shutdown during its release: %q with %d attempts, want pending with 0", status, attempts)
	}
}
