package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// ============================================================================
// Property: a completion (Ack/Nack) must never mutate a processing entry
// other than the presenter's own claim — INCLUDING when the claim changes
// between the completion's token check and its mutation.
// Surface: RedisQueue Ack/Nack trailing HDel (the check-then-act window).
//
// redis_stale_claim_test.go pins the fenced no-op when the re-claim
// completes BEFORE the stale worker's Ack/Nack starts (the HGet then sees
// the newer token and ownsClaim rejects). This file pins the residual
// window INSIDE one completion: Ack/Nack read the entry (HGet), compare
// tokens in-process, then HDel in a separate round trip — nothing binds
// the HDel to the entry whose token was compared. The mock's HDel seam
// injects the interleave (lease expiry → Reclaim → re-Dequeue) exactly
// between the two round trips: one Redis RTT of skew, the window a GC
// pause or Redis failover stretches in production. The stale HDel then
// deletes the NEWER claimant's entry: the job is on no list, invisible to
// Reclaim, and the new claimant's later Nack finds nothing (the
// documented idempotent no-op) — silent loss with Ack returning nil.
// ============================================================================

// interleaveHDel wraps mockRedis and fires a scripted callback just before
// its FIRST delete — the deterministic seam standing in for "another
// goroutine/server event landed between the completion's HGet and its
// delete". The hook disarms itself before running, so operations the script
// issues (Reclaim's own HDel, a re-Dequeue's HSet) pass through untouched.
//
// Both delete entry points are hooked: HDel for a client with no atomic
// capability, and HDelIfEqual for one that implements [CompareAndDeleter].
// The seam has to sit on whichever call actually performs the deletion,
// otherwise the interleave lands somewhere the completion never observes and
// the test proves nothing.
type interleaveHDel struct {
	*mockRedis
	before func()
}

func (f *interleaveHDel) fire() {
	if f.before != nil {
		hook := f.before
		f.before = nil
		hook()
	}
}

func (f *interleaveHDel) HDel(ctx context.Context, key string, fields ...string) error {
	f.fire()
	return f.mockRedis.HDel(ctx, key, fields...)
}

func (f *interleaveHDel) HDelIfEqual(ctx context.Context, key, field, expect string) (bool, error) {
	f.fire()
	return f.mockRedis.HDelIfEqual(ctx, key, field, expect)
}

// raceClaimInterleave arms the delete seam so that between the presenter's
// token check and its delete: the lease expires, Reclaim re-enqueues the
// entry, and W2 re-claims with a fresh token. The returned pointer is filled
// in when the hook fires inside the completion under test — it has to be a
// pointer, since the hook runs long after this function has returned.
func raceClaimInterleave(t *testing.T, q *RedisQueue, gate *interleaveHDel, now *time.Time) *Job {
	t.Helper()
	ctx := context.Background()
	w2 := &Job{}
	gate.before = func() {
		*now = now.Add(2 * time.Minute) // the 1m lease expires under the stale completion
		if _, err := q.Reclaim(ctx); err != nil {
			t.Fatalf("interleave reclaim: %v", err)
		}
		claimed, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("interleave re-dequeue: %v", err)
		}
		*w2 = claimed
		if w2.ClaimToken == "" {
			t.Fatalf("interleave re-claim minted no token")
		}
	}
	return w2
}

// assertNewerClaimSurvives reads the processing entry for jobID and fails
// unless it is present and carries exactly the newer claim's token.
func assertNewerClaimSurvives(t *testing.T, r *mockRedis, jobID string, newer *Job) {
	t.Helper()
	if newer.ClaimToken == "" {
		t.Fatalf("the interleave never ran: no re-claim happened, so this asserts nothing")
	}
	raw, err := r.HGet(context.Background(), "jobs:processing", jobID)
	if errors.Is(err, ErrRedisEmpty) {
		t.Fatalf("completion's trailing HDel deleted the NEWER claim's processing entry for %q: the new claimant is in-flight with no record — a crash leaves the job on no list and invisible to Reclaim, while the completion returned nil (silent loss)", jobID)
	}
	if err != nil {
		t.Fatalf("read processing entry: %v", err)
	}
	var entry processingEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("unmarshal processing entry: %v", err)
	}
	var cur Job
	if err := json.Unmarshal([]byte(entry.Job), &cur); err != nil {
		t.Fatalf("unmarshal claimed job: %v", err)
	}
	if cur.ClaimToken != newer.ClaimToken {
		t.Fatalf("processing entry token = %q, want the newer claim's %q", cur.ClaimToken, newer.ClaimToken)
	}
}

// TestAckDoesNotDeleteNewerClaimEntry: W1's Ack passes the token check
// against its own still-recorded entry, then Reclaim + a re-Dequeue land
// before its HDel. The HDel must not delete the newer claim's entry.
func TestAckDoesNotDeleteNewerClaimEntry(t *testing.T) {
	r := newMockRedis()
	gate := &interleaveHDel{mockRedis: r}
	q := NewRedisQueue(gate, "jobs")
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()
	q.SetVisibilityTimeout(time.Minute)

	if err := q.Enqueue(ctx, Job{ID: "pay", Type: "x", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	w1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("W1 dequeue: %v", err)
	}

	w2 := raceClaimInterleave(t, q, gate, &now)

	if err := q.Ack(ctx, w1); err != nil {
		t.Fatalf("Ack inside the race window must stay a no-op, not error: %v", err)
	}
	assertNewerClaimSurvives(t, r, "pay", w2)

	// Consequence of the entry surviving: W2 crashes, Reclaim must still
	// resurrect the job instead of it being lost with no list membership.
	now = now.Add(2 * time.Minute)
	n, err := q.Reclaim(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reclaim after W2 crash recovered %d jobs (err %v), want 1 — job lost", n, err)
	}
	revived, err := q.Dequeue(ctx)
	if err != nil || revived.ID != "pay" {
		t.Fatalf("revived = %+v (%v), want pay — job was lost", revived, err)
	}
}

// TestNackDoesNotDeleteNewerClaimEntry: same window on Nack's trailing
// HDel (after its re-enqueue push). Nack's own push may duplicate the job
// on the main list — this queue is at-least-once and a duplicate is in
// contract; deleting the newer claim's processing entry is not.
func TestNackDoesNotDeleteNewerClaimEntry(t *testing.T) {
	r := newMockRedis()
	gate := &interleaveHDel{mockRedis: r}
	q := NewRedisQueue(gate, "jobs")
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()
	q.SetVisibilityTimeout(time.Minute)

	if err := q.Enqueue(ctx, Job{ID: "pay", Type: "x", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	w1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("W1 dequeue: %v", err)
	}

	w2 := raceClaimInterleave(t, q, gate, &now)

	if err := q.Nack(ctx, w1); err != nil {
		t.Fatalf("Nack inside the race window: %v", err)
	}
	assertNewerClaimSurvives(t, r, "pay", w2)

	// Consequence: W2 crashes, the surviving entry must be reclaimable.
	now = now.Add(2 * time.Minute)
	n, err := q.Reclaim(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reclaim after W2 crash recovered %d jobs (err %v), want 1 — job lost", n, err)
	}
}

// ============================================================================
// Property: a malformed job record is QUARANTINED, never silently
// destroyed. Dequeue's own contract (pinned in queue_security_test.go:
// KeepsSkippedOnBadJSON / QuarantinesBadJSON) moves an unparseable main-
// list entry to the dead-letter list instead of dropping it — the
// no-silent-loss stance Nack, Reclaim and Replay all repeat. The
// processing hash is the ONLY durable copy of a claimed job, so Reclaim's
// corrupt-entry arm is the highest-stakes instance of the property: it
// currently HDel's the entry outright. The bytes (a claimed job's last
// copy) vanish with no dead-letter, no log, while Reclaim reports
// success.
// ============================================================================

// TestReclaimKeepsCorruptEntryObservable: a processing entry that rotted
// (partial write, version skew, external tampering) must survive Reclaim
// somewhere observable — quarantined to the dead-letter list like
// Dequeue's bad-JSON path, or left in place — never silently deleted.
func TestReclaimKeepsCorruptEntryObservable(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "jobs")
	ctx := context.Background()

	const raw = `{"job":"pay","expiresAt":` // truncated JSON, unparseable as a processingEntry
	r.mu.Lock()
	if r.hashes[q.processingQueue] == nil {
		r.hashes[q.processingQueue] = map[string]string{}
	}
	r.hashes[q.processingQueue]["pay"] = raw
	r.mu.Unlock()

	if _, err := q.Reclaim(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	// Somewhere observable: still in the processing hash, or moved to the
	// dead-letter list.
	r.mu.Lock()
	_, stillTracked := r.hashes[q.processingQueue]["pay"]
	dlq := append([]string(nil), r.lists[q.deadLetterQueue]...)
	r.mu.Unlock()

	quarantined := false
	for _, v := range dlq {
		if v == raw {
			quarantined = true
		}
	}
	if !stillTracked && !quarantined {
		t.Fatalf("SECURITY: [queue] Reclaim silently deleted a corrupt processing entry: the claimed job's only durable copy is gone (not in the processing hash, not in %q, no dead-letter, no log) while Reclaim reported success — the exact silent loss the queue's own quarantine contract forbids on the main list",
			q.deadLetterQueue)
	}
}
