package queue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// MaxAge ends the retries whichever way the attempt failed. It was read
// only in settleFailure, so a claim whose worker died (or whose handler
// outran the lease) never met it: the job was re-delivered through the
// claim's lease-expiry clause after every lost worker until its attempts
// ran out, however far past CreatedAt+MaxAge it was. The pre-claim sweep
// now dead-letters an expired claim that is past its age, the same outcome
// a Nack at that moment has.
func TestMaxAgeAppliesWhenLeaseExpires(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		assertMaxAgeOnLeaseExpiry(t, func(t *testing.T, opts ...DBQueueOption) *DBQueue {
			_, q := openDBQueueOpts(t, opts...)
			return q
		})
	})
	t.Run("postgres", func(t *testing.T) {
		pgtest.BaseDSN(t) // skip here, once, when there is no Postgres
		assertMaxAgeOnLeaseExpiry(t, func(t *testing.T, opts ...DBQueueOption) *DBQueue {
			// A schema per case: the cases reuse a job id.
			q, err := NewDBQueue(pgtest.DB(t), opts...)
			if err != nil {
				t.Fatal(err)
			}
			return q
		})
	})
}

func assertMaxAgeOnLeaseExpiry(t *testing.T, open func(*testing.T, ...DBQueueOption) *DBQueue) {
	ctx := context.Background()
	// start opens a queue on a fake clock and leaves one claimed job whose
	// worker never settles it.
	start := func(t *testing.T, lease, maxAge time.Duration) (*DBQueue, *fakeClock, Job) {
		t.Helper()
		q := open(t, WithWorkers(0), WithLeaseTimeout(lease))
		clock := newFakeClock()
		q.now = clock.Now
		if err := q.Enqueue(ctx, Job{ID: "otp", Type: "send-code", MaxAttempts: 25, MaxAge: maxAge}); err != nil {
			t.Fatal(err)
		}
		claimed, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return q, clock, claimed
	}

	t.Run("lost worker past the age", func(t *testing.T) {
		q, clock, _ := start(t, 2*time.Minute, time.Minute)
		clock.Advance(5 * time.Minute) // lease (2m) gone, job 5 minutes old
		if again, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
			t.Fatalf("a job with MaxAge 1m was handed out again 5m after it was created (job %q, err %v)", again.ID, err)
		}
		got := stored(t, q, "otp")
		if got.Status != "failed" || got.Attempts != 1 {
			t.Fatalf("status %q after %d attempts, want failed after 1", got.Status, got.Attempts)
		}
		if !strings.Contains(got.LastError, "lease expired past MaxAge") {
			t.Errorf("last error %q, want the lease expiry past MaxAge named", got.LastError)
		}
		if !got.UpdatedAt.Equal(clock.Now()) {
			t.Errorf("updated_at %s, want the sweep's %s", got.UpdatedAt, clock.Now())
		}
	})

	// The same instant settled through Nack: the outcome the sweep matches.
	t.Run("nack past the age", func(t *testing.T) {
		q, clock, claimed := start(t, 10*time.Minute, time.Minute)
		clock.Advance(5 * time.Minute)
		if err := q.Nack(ctx, claimed); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, q, "otp"); got.Status != "failed" {
			t.Fatalf("status %q, want failed", got.Status)
		}
	})

	t.Run("lost worker inside the age is re-delivered", func(t *testing.T) {
		q, clock, _ := start(t, 2*time.Minute, time.Hour)
		clock.Advance(5 * time.Minute)
		again, err := q.Dequeue(ctx)
		if err != nil || again.ID != "otp" {
			t.Fatalf("a 5-minute-old job with MaxAge 1h was not re-delivered after its lease ran out: job %q, err %v", again.ID, err)
		}
	})

	t.Run("a live claim past the age is left alone", func(t *testing.T) {
		q, clock, _ := start(t, 10*time.Minute, time.Minute)
		clock.Advance(5 * time.Minute) // past the age, inside the lease
		if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
			t.Fatalf("dequeue: %v", err)
		}
		if got := stored(t, q, "otp"); got.Status != "claimed" {
			t.Fatalf("status %q, want claimed: the sweep settled a claim whose lease has not run out", got.Status)
		}
	})
}
