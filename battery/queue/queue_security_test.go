package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Property: a handler that panics must not destroy worker capacity or crash
// the process. The panic must be recovered and routed through the retry path.
// Surfaces: DBQueue.workerLoop, MemoryQueue.processJob.
// ============================================================================

// TestDBWorkerSurvivesHandlerPanic asserts a panicking handler does not kill
// the worker pool: a subsequent good job still gets processed, and the
// poisoned job is nacked rather than left claimed/leaked.
func TestDBWorkerSurvivesHandlerPanic(t *testing.T) {
	db, q := openDBQueue(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var good atomic.Int32
	q.RegisterHandler("boom", func(_ context.Context, _ Job) error {
		var m map[string]int
		m["x"] = 1 // nil-map assignment → panic
		return nil
	})
	q.RegisterHandler("ok", func(_ context.Context, _ Job) error {
		good.Add(1)
		return nil
	})

	q.Start(ctx)

	// The poison job is processed first by the single worker. If the panic
	// kills the goroutine, the "ok" job is never processed.
	if err := q.Enqueue(ctx, Job{Type: "boom", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue boom: %v", err)
	}
	if err := q.Enqueue(ctx, Job{Type: "ok"}); err != nil {
		t.Fatalf("enqueue ok: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for good.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	q.Close()

	if good.Load() < 1 {
		t.Fatalf("worker died after handler panic — good job never processed")
	}
	// The poisoned job (MaxAttempts=1) must not be stuck in 'claimed'; the
	// recovered panic should have nacked it to 'failed'.
	var stuck int
	db.QueryRow("SELECT COUNT(*) FROM queue_jobs WHERE type='boom' AND status='claimed'").Scan(&stuck)
	if stuck != 0 {
		t.Fatalf("panicked job left in 'claimed' state (leaked), count=%d", stuck)
	}
}

// TestMemoryWorkerSurvivesHandlerPanic asserts a panicking handler in the
// MemoryQueue worker pool does not crash the process or kill the worker.
func TestMemoryWorkerSurvivesHandlerPanic(t *testing.T) {
	q := NewMemoryQueue(1)

	var good atomic.Int32
	q.RegisterHandler("boom", func(_ context.Context, _ Job) error {
		var s []int
		_ = s[5] // slice OOB → panic
		return nil
	})
	q.RegisterHandler("ok", func(_ context.Context, _ Job) error {
		good.Add(1)
		return nil
	})
	q.Start()

	_ = q.Enqueue(context.Background(), Job{Type: "boom", MaxAttempts: 1})
	_ = q.Enqueue(context.Background(), Job{Type: "ok"})

	deadline := time.Now().Add(2 * time.Second)
	for good.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	q.Close()

	if good.Load() < 1 {
		t.Fatalf("memory worker died after handler panic — good job never processed")
	}
}

// ============================================================================
// Property: a job claimed by a worker that then crashes must eventually become
// re-eligible — in-flight work must not be lost forever.
// Surface: DBQueue dequeue / eligibleWhere lease reclaim.
// ============================================================================

// TestDBReclaimsStaleClaimedJob simulates a worker that claimed a job and
// died before Ack/Nack: the row sits in 'claimed'. After the lease expires
// the job must be re-dequeued, not lost.
func TestDBReclaimsStaleClaimedJob(t *testing.T) {
	db, q := openDBQueue(t, 0)
	q.SetLeaseTimeout(50 * time.Millisecond)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{ID: "leaky", Type: "x", MaxAttempts: 5}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Claim it, then "crash" (never Ack/Nack).
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("first dequeue: %v", err)
	}
	if job.ID != "leaky" {
		t.Fatalf("unexpected job: %s", job.ID)
	}

	// Before the lease expires the row is not re-eligible.
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("claimed job re-dequeued before lease expiry: %v", err)
	}

	// Wait for the lease to expire, then it must be reclaimable.
	time.Sleep(80 * time.Millisecond)
	reclaimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("stale claimed job was lost — not reclaimed after lease expiry: %v", err)
	}
	if reclaimed.ID != "leaky" {
		t.Fatalf("expected to reclaim 'leaky', got %s", reclaimed.ID)
	}

	// Sanity: the row still exists and is now claimed again (not duplicated).
	var rows int
	db.QueryRow("SELECT COUNT(*) FROM queue_jobs WHERE id='leaky'").Scan(&rows)
	if rows != 1 {
		t.Fatalf("expected exactly 1 row for reclaimed job, got %d", rows)
	}
}

// TestDBReclaimRespectsMaxAttempts asserts a stale-claimed job whose attempts
// already hit max is NOT re-run indefinitely — it must fail closed.
func TestDBReclaimRespectsMaxAttempts(t *testing.T) {
	_, q := openDBQueue(t, 0)
	q.SetLeaseTimeout(50 * time.Millisecond)
	ctx := context.Background()

	// MaxAttempts=1: the single claim exhausts it.
	q.Enqueue(ctx, Job{ID: "exhausted", Type: "x", MaxAttempts: 1})
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("claim: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("exhausted stale job should not be reclaimed, got %v", err)
	}
}

// ============================================================================
// Property: Redis Dequeue must never lose jobs it already RPop'd.
// Surface: RedisQueue.Dequeue malformed-entry exit path.
// ============================================================================

// TestRedisDequeueKeepsSkippedOnBadJSON puts a valid non-matching job behind a
// malformed entry. A type-filtered Dequeue must not silently drop the valid
// job when it hits the malformed one.
func TestRedisDequeueKeepsSkippedOnBadJSON(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	// Producer order (RPop is FIFO from the tail): valid "sms" first, then a
	// malformed entry. Filtering for "email" skips the sms job, then trips on
	// the malformed entry.
	_ = q.Enqueue(ctx, Job{ID: "keepme", Type: "sms"})
	// Inject a malformed list entry directly (LPush prepends → RPop'd after).
	_ = r.LPush(ctx, "test", "{not valid json")

	_, err := q.Dequeue(ctx, "email")
	if err == nil {
		t.Fatalf("expected an error from malformed entry")
	}

	// The valid "sms" job must still be retrievable — it must not have been
	// dropped along with the malformed entry.
	job, err := q.Dequeue(ctx, "sms")
	if err != nil {
		t.Fatalf("valid skipped job was lost on malformed entry: %v", err)
	}
	if job.ID != "keepme" {
		t.Fatalf("expected to recover 'keepme', got %q", job.ID)
	}
}

// TestRedisDequeueQuarantinesBadJSON asserts a malformed entry is moved to the
// dead-letter queue rather than silently re-circulating or being lost.
func TestRedisDequeueQuarantinesBadJSON(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	_ = r.LPush(ctx, "test", "{garbage")

	if _, err := q.Dequeue(ctx); err == nil {
		t.Fatalf("expected error from malformed entry")
	}

	r.mu.Lock()
	dlq := len(r.lists["test:dead"])
	main := len(r.lists["test"])
	r.mu.Unlock()
	if dlq != 1 {
		t.Fatalf("malformed entry should be quarantined to DLQ, dead len=%d", dlq)
	}
	if main != 0 {
		t.Fatalf("malformed entry should not remain on main queue, main len=%d", main)
	}
}

// ============================================================================
// Property: a recorded visibility timeout must actually re-deliver abandoned
// in-flight jobs. Surface: RedisQueue.Reclaim.
// ============================================================================

// TestRedisReclaimRedeliversExpired asserts that a job left in the processing
// hash past its visibility timeout is re-enqueued by Reclaim.
func TestRedisReclaimRedeliversExpired(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	q.SetVisibilityTimeout(20 * time.Millisecond)
	ctx := context.Background()

	_ = q.Enqueue(ctx, Job{ID: "abandoned", Type: "x"})
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if job.ID != "abandoned" {
		t.Fatalf("unexpected job %q", job.ID)
	}

	// Worker "crashes" — never Ack/Nack. Before expiry, Reclaim is a no-op.
	n, err := q.Reclaim(ctx)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected nothing reclaimed before expiry, got %d", n)
	}

	time.Sleep(40 * time.Millisecond)
	n, err = q.Reclaim(ctx)
	if err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reclaimed after expiry, got %d", n)
	}

	// The job must be re-dequeuable from the main queue.
	redelivered, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("abandoned job not re-delivered after Reclaim: %v", err)
	}
	if redelivered.ID != "abandoned" {
		t.Fatalf("expected re-delivered 'abandoned', got %q", redelivered.ID)
	}
}
