package queue

import (
	"context"
	"errors"
	"strconv"
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

	waitFor(t, func() bool { return good.Load() >= 1 }, 5*time.Second,
		"worker died after handler panic — good job never processed")
	q.Close()
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

	// Close drains the channel and waits for the worker: if the panic killed
	// the worker goroutine, Close would hang (test timeout) and the good job
	// would never be processed.
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
	q.SetLeaseTimeout(time.Minute)
	// Fake clock: no workers are running, so the single test goroutine is the
	// only reader of q.now — lease expiry is asserted by advancing the clock,
	// not by sleeping.
	now := time.Now()
	q.now = func() time.Time { return now }
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

	// Before the lease expires the row is not re-eligible. The clock is
	// frozen, so this cannot race the expiry.
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("claimed job re-dequeued before lease expiry: %v", err)
	}

	// Advance the clock past the lease; the job must be reclaimable.
	now = now.Add(time.Minute + time.Second)
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
	q.SetLeaseTimeout(time.Minute)
	// Fake clock (single test goroutine, no workers) — see
	// TestDBReclaimsStaleClaimedJob.
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()

	// MaxAttempts=1: the single claim exhausts it.
	q.Enqueue(ctx, Job{ID: "exhausted", Type: "x", MaxAttempts: 1})
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Even with the lease long expired, the exhausted job must stay dead.
	now = now.Add(time.Hour)
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
// Property: a type-filtered MemoryQueue.Dequeue must never lose the valid,
// non-matching jobs it drained while searching — even when the bounded jobChan
// is full at re-enqueue time. Surface: MemoryQueue.Dequeue type-filter branch.
// ============================================================================

// TestMemoryDequeueKeepsSkippedUnderLoad runs a type-filtered Dequeue against a
// near-full jobChan while a concurrent producer keeps refilling it. The drained
// non-matching jobs must never be silently dropped: with a non-blocking
// re-enqueue, a producer that steals the freed slot causes permanent job loss.
// We assert the total job count is conserved across the drain/re-enqueue cycle.
func TestMemoryDequeueKeepsSkippedUnderLoad(t *testing.T) {
	q := NewMemoryQueue(0) // no workers — manual consumption only
	ctx := context.Background()

	const cap = 1024 // jobChan capacity

	// Seed the channel completely full with non-matching jobs.
	for i := 0; i < cap; i++ {
		if err := q.Enqueue(ctx, Job{ID: fmtID(i), Type: "sms"}); err != nil {
			t.Fatalf("seed enqueue %d: %v", i, err)
		}
	}

	// A producer hammers Enqueue concurrently with the type-filtered Dequeue,
	// racing to grab any slot the drain frees up. Count how many it lands.
	var produced atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < cap*4; i++ {
			if err := q.Enqueue(ctx, Job{ID: "p" + strconv.Itoa(i), Type: "sms"}); err == nil {
				produced.Add(1)
			}
		}
	}()

	// Drain looking for an absent type: every job is RPop'd into skipped and
	// must be re-enqueued. Run it repeatedly to widen the race window.
	for i := 0; i < 50; i++ {
		if _, err := q.Dequeue(ctx, "email"); !errors.Is(err, ErrNoJob) && err != nil {
			t.Fatalf("filtered dequeue: %v", err)
		}
	}
	<-done

	// Total surviving jobs must equal seeded + producer-acked. If the
	// non-blocking re-enqueue dropped any drained job, the count comes up short.
	want := cap + int(produced.Load())
	got := 0
	for {
		_, err := q.Dequeue(ctx, "sms")
		if errors.Is(err, ErrNoJob) {
			break
		}
		if err != nil {
			t.Fatalf("drain dequeue: %v", err)
		}
		got++
	}
	if got != want {
		t.Fatalf("type-filtered Dequeue lost jobs under load: have %d, expected %d (lost %d)", got, want, want-got)
	}
}

func fmtID(i int) string { return "j" + strconv.Itoa(i) }

// ============================================================================
// Property: a recorded visibility timeout must actually re-deliver abandoned
// in-flight jobs. Surface: RedisQueue.Reclaim.
// ============================================================================

// TestRedisReclaimRedeliversExpired asserts that a job left in the processing
// hash past its visibility timeout is re-enqueued by Reclaim.
func TestRedisReclaimRedeliversExpired(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	q.SetVisibilityTimeout(time.Minute)
	// Fake clock: Start is never called, so the single test goroutine is the
	// only reader of q.now — expiry is asserted by advancing the clock.
	now := time.Now()
	q.now = func() time.Time { return now }
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
	// The clock is frozen, so this cannot race the visibility timeout.
	n, err := q.Reclaim(ctx)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected nothing reclaimed before expiry, got %d", n)
	}

	// Advance the clock past the visibility timeout.
	now = now.Add(time.Minute + time.Second)
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
