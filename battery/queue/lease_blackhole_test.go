package queue

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// openDBQueueWithLeaseLogging is openDBQueueWithLogger plus a short lease, so
// lease-expiry dead-lettering can be driven by a fake clock and its ERROR log
// asserted in one place.
func openDBQueueWithLeaseLogging(t *testing.T) (*sql.DB, *DBQueue, *bytes.Buffer) {
	t.Helper()
	db, q, buf := openDBQueueWithLogger(t, 0)
	q.SetLeaseTimeout(time.Minute)
	return db, q, buf
}

// ============================================================================
// Property: a worker crash on the FINAL permitted attempt must dead-letter
// the job, never strand it in 'claimed'.
// Surface: DBQueue dequeue / eligibleWhere lease reclaim + dead-letter sweep.
//
// Dequeue increments attempts at claim, and the reclaim clause requires
// attempts < max_attempts — so an expired lease on the final attempt matches
// no path: not re-delivered, never Nacked (so never 'failed'), invisible to
// Replay. A lease expiry on the final attempt is the crash equivalent of a
// terminal Nack and must land in the same dead-letter state.
// ============================================================================

// TestDBFinalAttemptLeaseExpiryDeadLetters simulates a worker that claimed a
// MaxAttempts=1 job and crashed: after the lease expires the job must be
// status='failed' (observable in Stats/ListJobs, rescuable via Replay), not
// stranded in 'claimed'.
func TestDBFinalAttemptLeaseExpiryDeadLetters(t *testing.T) {
	db, q, buf := openDBQueueWithLeaseLogging(t)
	// Fake clock: no workers are running, so the single test goroutine is the
	// only reader of q.now — lease expiry is asserted by advancing the clock,
	// not by sleeping.
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "critical", Type: "x", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("first dequeue: %v", err)
	}
	if job.Attempts != 1 {
		t.Fatalf("attempts after claim = %d, want 1 (the final attempt)", job.Attempts)
	}
	// Worker "crashes" — never Ack/Nack.

	// Before the lease expires nothing may change (clock is frozen; no race).
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("pre-expiry dequeue: %v", err)
	}
	var status string
	db.QueryRow("SELECT status FROM queue_jobs WHERE id='critical'").Scan(&status)
	if status != "claimed" {
		t.Fatalf("pre-expiry status = %q, want claimed", status)
	}

	// Advance the clock past the lease. The final-attempt crash must NOT
	// re-deliver (fail closed, like a terminal Nack)…
	now = now.Add(time.Minute + time.Second)
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("expired final-attempt job was re-delivered: %v", err)
	}
	// …and must be dead-lettered, not stranded.
	db.QueryRow("SELECT status FROM queue_jobs WHERE id='critical'").Scan(&status)
	if status != "failed" {
		t.Fatalf("post-expiry status = %q, want failed — job stranded in claimed (black hole)", status)
	}

	// The dead-letter is observable through the Browsable surface.
	failed, err := q.ListJobs(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != "critical" {
		t.Fatalf("ListJobs(failed) = %+v, want the critical job", failed)
	}
	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["failed"] != 1 || stats["claimed"] != 0 {
		t.Fatalf("stats = %v, want failed=1 claimed=0", stats)
	}

	// Replay can rescue it — attempts reset, immediately claimable again.
	if err := q.Replay(ctx, "critical"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	replayed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue after replay: %v", err)
	}
	if replayed.ID != "critical" || replayed.Attempts != 1 {
		t.Fatalf("replayed job = %+v, want critical with a fresh attempt", replayed)
	}

	// The silent loss is also logged at ERROR, matching the worker loop's
	// dead-letter records.
	if err := q.Ack(ctx, replayed); err != nil {
		t.Fatalf("ack replayed: %v", err)
	}
	assertLogLine(t, buf, "ERROR", "queue: lease expired on final attempt")
}

// ============================================================================
// Companion: a crash with attempts REMAINING still re-delivers (the reclaim
// path itself is unchanged by the dead-letter sweep).
// ============================================================================

func TestDBNonFinalLeaseExpiryStillReclaims(t *testing.T) {
	_, q, _ := openDBQueueWithLeaseLogging(t)
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "retryable", Type: "x", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("first dequeue: %v", err)
	}
	// Crash on attempt 1 of 3 — must be re-delivered, not dead-lettered.
	now = now.Add(time.Minute + time.Second)
	reclaimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("expired non-final job was not reclaimed: %v", err)
	}
	if reclaimed.ID != "retryable" || reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed = %+v, want retryable attempts=2", reclaimed)
	}
}
