package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The dead-letter sweep moved a final-attempt claim whose lease expired to
// 'failed' and wrote nothing else: the job listed with the updated_at of
// its claim and the last_error of an earlier attempt (or none), so an
// operator reading ListJobs("failed") could not tell a crashed worker
// from a handler error. The sweep now records when and why, as every
// other settle does.
func TestLeaseSweepRecordsWhyAndWhen(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		_, q := openDBQueueOpts(t, WithWorkers(0), WithLeaseTimeout(time.Minute))
		assertLeaseSweepRecords(t, q)
	})
	t.Run("postgres", func(t *testing.T) {
		q, err := NewDBQueue(pgtest.DB(t), WithWorkers(0), WithLeaseTimeout(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		assertLeaseSweepRecords(t, q)
	})
}

func assertLeaseSweepRecords(t *testing.T, q *DBQueue) {
	clock := time.Now().UTC().Truncate(time.Second)
	q.now = func() time.Time { return clock }
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j", Type: "t", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Dequeue(ctx, "t"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute) // the worker died holding the claim
	if _, err := q.Dequeue(ctx, "t"); err != ErrNoJob {
		t.Fatalf("sweep pass: %v", err)
	}
	j := onlyJob(t, q, "failed")
	if !strings.Contains(j.LastError, "lease expired") {
		t.Errorf("last error %q, want the lease expiry named", j.LastError)
	}
	if !j.UpdatedAt.Equal(clock) {
		t.Errorf("updated_at %s, want the sweep's %s", j.UpdatedAt, clock)
	}
}

func TestDBStaleClaimCannotSettleTheNewOne(t *testing.T) {
	t.Run("delete", func(t *testing.T) { assertStaleClaimCannotSettle(t) })
	t.Run("done-retention", func(t *testing.T) { assertStaleClaimCannotSettle(t, WithDoneRetention(time.Hour)) })
}

func assertStaleClaimCannotSettle(t *testing.T, opts ...DBQueueOption) {
	_, q := openDBQueueOpts(t, append([]DBQueueOption{WithWorkers(0), WithLeaseTimeout(time.Minute)}, opts...)...)
	clock := time.Now().UTC().Truncate(time.Second)
	q.now = func() time.Time { return clock }
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j", Type: "t", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	stale, err := q.Dequeue(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := q.Dequeue(ctx, "t"); err != nil {
		t.Fatalf("re-claim after the lease expired: %v", err)
	}
	if err := q.Ack(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if j := onlyJob(t, q, "claimed"); j.ID != "j" {
		t.Errorf("the stale claim's Ack or Nack moved the live claim: %+v", j)
	}
}

// The job is re-claimed between the stale Nack's SELECT and its UPDATE.
func TestDBStaleNackLosesARaceToANewClaim(t *testing.T) {
	for name, maxAge := range map[string]time.Duration{"retry": 0, "dead": 5 * time.Minute} {
		t.Run(name, func(t *testing.T) {
			_, q := openDBQueueOpts(t, WithWorkers(0), WithLeaseTimeout(time.Minute), WithBackoff(10*time.Minute, time.Hour), WithRetryJitter(0.1))
			clock := time.Now().UTC().Truncate(time.Second)
			q.now = func() time.Time { return clock }
			ctx := context.Background()
			if err := q.Enqueue(ctx, Job{ID: "j", Type: "t", MaxAttempts: 5, MaxAge: maxAge}); err != nil {
				t.Fatal(err)
			}
			stale, err := q.Dequeue(ctx, "t")
			if err != nil {
				t.Fatal(err)
			}
			reclaimed := false
			q.jitterRand = func() float64 {
				clock = clock.Add(2 * time.Minute)
				if _, err := q.Dequeue(ctx, "t"); err != nil {
					t.Fatalf("re-claim mid-settle: %v", err)
				}
				reclaimed = true
				return 0.5
			}
			if err := q.Nack(ctx, stale); err != nil {
				t.Fatal(err)
			}
			if !reclaimed {
				t.Fatal("the settle never reached the backoff")
			}
			if j := onlyJob(t, q, "claimed"); j.ID != "j" {
				t.Errorf("the stale claim's Nack moved the live claim: %+v", j)
			}
		})
	}
}
