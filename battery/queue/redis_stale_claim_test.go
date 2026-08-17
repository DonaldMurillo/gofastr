package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ============================================================================
// Property: a completion (Ack/Nack) presented by a claim that is no longer
// current must not mutate the current claimant's state.
// Surface: RedisQueue Ack/Nack claim fencing (Job.ClaimToken).
//
// W1 claims J and stalls past the visibility timeout; Reclaim re-enqueues J;
// W2 re-claims it. The processing entry now belongs to W2. If W1 then wakes
// and Acks/Nacks by bare job ID, it deletes or re-enqueues W2's entry — and
// when W2 later crashes there is nothing left to reclaim: the job is on no
// list, in no hash, never dead-lettered. The claim token mints a fresh
// identity per claim so a stale holder's completion is a fenced no-op.
// ============================================================================

// claimByTwoWorkers drives the shared scenario: enqueue, W1 claims, lease
// expires, Reclaim re-enqueues, W2 re-claims. Returns both claims (W1's is
// the now-stale one). The fake clock (*now) is left just after W2's claim.
func claimByTwoWorkers(t *testing.T, q *RedisQueue, now *time.Time) (w1, w2 Job) {
	t.Helper()
	ctx := context.Background()
	q.SetVisibilityTimeout(time.Minute)

	if err := q.Enqueue(ctx, Job{ID: "pay", Type: "x", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	w1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("W1 dequeue: %v", err)
	}
	if w1.ClaimToken == "" {
		t.Fatalf("claim minted no ClaimToken — fencing impossible")
	}
	// W1 stalls past the visibility timeout; the entry is reclaimed and
	// re-claimed by W2.
	*now = now.Add(2 * time.Minute)
	if _, err := q.Reclaim(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	w2, err = q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("W2 dequeue: %v", err)
	}
	if w2.ID != w1.ID {
		t.Fatalf("W2 claimed %q, want the same job %q", w2.ID, w1.ID)
	}
	if w2.ClaimToken == "" || w2.ClaimToken == w1.ClaimToken {
		t.Fatalf("re-claim reused W1's token %q — fencing broken", w2.ClaimToken)
	}
	return w1, w2
}

// TestRedisStaleAckCannotDeleteReclaimant: W1's late Ack must NOT delete
// W2's processing entry. After W2 crashes (visibility expiry + Reclaim), the
// job must still exist — re-delivered on the main list, not lost.
func TestRedisStaleAckCannotDeleteReclaimant(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "jobs")
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()

	w1, _ := claimByTwoWorkers(t, q, &now)

	// W1's handler finishes late and Acks the claim it no longer holds.
	if err := q.Ack(ctx, w1); err != nil {
		t.Fatalf("stale ack must be a no-op, not an error: %v", err)
	}

	// W2's entry must survive the stale Ack — it is the current claim.
	if _, err := r.HGet(ctx, "jobs:processing", w1.ID); err != nil {
		t.Fatalf("stale Ack deleted the re-claimant's processing entry: %v", err)
	}
	if got := q.StaleClaimCount(); got != 1 {
		t.Fatalf("StaleClaimCount = %d, want 1 (stale completion must be observable)", got)
	}

	// W2 crashes. After its visibility window the job must be resurrected —
	// not vanished from every list and hash.
	now = now.Add(2 * time.Minute)
	n, err := q.Reclaim(ctx)
	if err != nil {
		t.Fatalf("reclaim after W2 crash: %v", err)
	}
	if n != 1 {
		t.Fatalf("reclaim after W2 crash moved %d jobs, want 1 — job was lost", n)
	}
	revived, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("revived job was not re-deliverable: %v", err)
	}
	if revived.ID != "pay" {
		t.Fatalf("revived job = %q, want pay", revived.ID)
	}
}

// TestRedisStaleNackCannotTouchReclaimant: W1's late Nack must NOT re-enqueue
// W2's in-flight job nor delete W2's entry — otherwise the job runs twice
// concurrently and W2's crash finds nothing to reclaim.
func TestRedisStaleNackCannotTouchReclaimant(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "jobs")
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()

	w1, w2 := claimByTwoWorkers(t, q, &now)

	// W1's handler fails late and Nacks the claim it no longer holds.
	if err := q.Nack(ctx, w1); err != nil {
		t.Fatalf("stale nack must be a no-op, not an error: %v", err)
	}

	// The main list must stay empty (no premature re-enqueue) and W2's
	// processing entry must survive.
	if got, err := r.LRange(ctx, "jobs", 0, -1); err != nil || len(got) != 0 {
		t.Fatalf("stale Nack re-enqueued the re-claimant's job: %v (%v)", got, err)
	}
	if _, err := r.HGet(ctx, "jobs:processing", w1.ID); err != nil {
		t.Fatalf("stale Nack deleted the re-claimant's processing entry: %v", err)
	}
	if got := q.StaleClaimCount(); got != 1 {
		t.Fatalf("StaleClaimCount = %d, want 1 (stale completion must be observable)", got)
	}

	// Sanity: the CURRENT claimant (W2) can still complete normally — the
	// fenced nack re-enqueues its job for the next attempt.
	if err := q.Nack(ctx, w2); err != nil {
		t.Fatalf("current claimant's nack failed: %v", err)
	}
	if _, err := r.HGet(ctx, "jobs:processing", w2.ID); !errors.Is(err, ErrRedisEmpty) {
		t.Fatalf("current claimant's nack did not clear its entry (err=%v)", err)
	}
	retry, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("nacked job was not re-delivered: %v", err)
	}
	if retry.ID != "pay" || retry.Attempts != 3 {
		t.Fatalf("re-delivered = %+v, want pay attempts=3", retry)
	}
}

// TestRedisCurrentClaimAckStillWorks is the positive control: the CURRENT
// claimant's Ack removes the entry (fencing must not break the happy path),
// and double-acking stays an idempotent no-op.
func TestRedisCurrentClaimAckStillWorks(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "jobs")
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()
	q.SetVisibilityTimeout(time.Minute)

	_ = q.Enqueue(ctx, Job{ID: "happy", Type: "x"})
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if err := q.Ack(ctx, job); err != nil {
		t.Fatalf("current claimant's ack: %v", err)
	}
	if _, err := r.HGet(ctx, "jobs:processing", "happy"); !errors.Is(err, ErrRedisEmpty) {
		t.Fatalf("ack did not remove the processing entry (err=%v)", err)
	}
	// Double-ack is an idempotent no-op, and it is NOT a stale claim.
	if err := q.Ack(ctx, job); err != nil {
		t.Fatalf("double ack: %v", err)
	}
	if got := q.StaleClaimCount(); got != 0 {
		t.Fatalf("StaleClaimCount = %d after double ack, want 0", got)
	}
}
