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
// Property: a late Ack from a crashed worker must not erase the dead-letter
// record the sweep just wrote.
// Surface: DBQueue Ack × deadLetterExpiredFinalClaims interaction.
// ============================================================================

// TestLateAckKeepsDeadLetterRow pins Ack's contract against the sweep: Ack
// retires a job only while its claim is live (status='claimed'). A worker
// whose lease expired on the final attempt may wake long after the sweep
// dead-lettered its job and report success — that Ack must be a no-op, not a
// DELETE.
//
// Why no-op rather than delete, even though the work did finish: the sweep
// already logged the dead-letter at ERROR and Stats counted a failure;
// deleting the row afterwards leaves that signal unreconcilable. The
// operator's sanctioned cleanup is Replay, which makes the row claimable so
// a live worker Ack deletes it through the normal route. This also matches
// RedisQueue, where a stale claim's Ack is a fenced no-op and the
// dead-letter entry survives — the unfenced DB backend is the one that most
// needs the conservative delete.
func TestLateAckKeepsDeadLetterRow(t *testing.T) {
	db, q, _ := openDBQueueWithLeaseLogging(t)
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "lateacker", Type: "x", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}

	// Lease expires on the final attempt; the sweep dead-letters the row.
	now = now.Add(time.Minute + time.Second)
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("post-expiry dequeue: %v", err)
	}

	// The crashed worker finally wakes and reports success. This must not
	// erase the failure record.
	if err := q.Ack(ctx, job); err != nil {
		t.Fatalf("late ack: %v", err)
	}
	failed, err := q.ListJobs(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != "lateacker" {
		t.Fatalf("ListJobs(failed) = %+v, want the dead-lettered job to survive the late Ack", failed)
	}
	var status string
	db.QueryRow("SELECT status FROM queue_jobs WHERE id='lateacker'").Scan(&status)
	if status != "failed" {
		t.Fatalf("status after late ack = %q, want failed", status)
	}

	// The operator reconciles explicitly: Replay makes the row claimable, and
	// the OWNING claim's Ack still retires it — the predicate fences stale
	// completions only, not live ones.
	if err := q.Replay(ctx, "lateacker"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	replayed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue after replay: %v", err)
	}
	if err := q.Ack(ctx, replayed); err != nil {
		t.Fatalf("ack after replay: %v", err)
	}
	left, err := q.ListJobs(ctx, "", 10)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("jobs after owning ack = %+v, want none", left)
	}
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
