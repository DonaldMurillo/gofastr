package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A zero or negative lease made every claim abandoned the instant it was
// written: the lease-expiry clause put the cutoff at now (or later), so the
// next poll re-claimed a job its worker was still running and ran it
// again concurrently, and on the final attempt the dead-letter sweep
// failed a job that was about to succeed. WithLeaseTimeout(0) reads like
// "no lease"; it now keeps the five-minute default, as the outbox's and
// the durable scheduler's lease options already did.
func TestNonPositiveLeaseKeepsDefault(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...DBQueueOption) *DBQueue
	}{
		{"sqlite", func(t *testing.T, opts ...DBQueueOption) *DBQueue {
			_, q := openDBQueueOpts(t, opts...)
			return q
		}},
		{"postgres", func(t *testing.T, opts ...DBQueueOption) *DBQueue {
			q, err := NewDBQueue(pgtest.DB(t), opts...)
			if err != nil {
				t.Fatal(err)
			}
			return q
		}},
	}
	for _, dl := range dialects {
		for _, lease := range []time.Duration{0, -time.Minute} {
			for _, via := range []string{"option", "setter"} {
				for _, maxAttempts := range []int{1, 3} {
					t.Run(fmt.Sprintf("%s/%s/%s/max%d", dl.name, lease, via, maxAttempts), func(t *testing.T) {
						var q *DBQueue
						if via == "option" {
							q = dl.open(t, WithWorkers(0), WithLeaseTimeout(lease))
						} else {
							q = dl.open(t, WithWorkers(0))
							q.SetLeaseTimeout(lease)
						}
						clock := time.Now().UTC().Truncate(time.Second)
						q.now = func() time.Time { return clock }
						ctx := context.Background()
						if err := q.Enqueue(ctx, Job{ID: "j", Type: "t", MaxAttempts: maxAttempts}); err != nil {
							t.Fatal(err)
						}
						first, err := q.Dequeue(ctx, "t")
						if err != nil {
							t.Fatalf("first claim: %v", err)
						}
						clock = clock.Add(time.Second) // the handler is still running
						if again, err := q.Dequeue(ctx, "t"); !errors.Is(err, ErrNoJob) {
							t.Fatalf("second claim of a live job: %+v, %v; want ErrNoJob", again, err)
						}
						if err := q.Ack(ctx, first); err != nil {
							t.Fatal(err)
						}
						if n := countJobs(t, q.db, q.qt()); n != 0 {
							t.Fatalf("%d rows after the live claim's Ack, want 0 (the sweep dead-lettered it)", n)
						}
					})
				}
			}
		}
	}
}

func countJobs(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
