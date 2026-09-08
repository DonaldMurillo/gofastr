package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Pins: a stale Reclaim sweep never deletes a newer claimant's processing entry (round 5, fixed).
// Property: claim/lease mutations are fenced — a delete following a stale
// observation must never remove a NEWER claimant's processing entry. The
// package states the property itself on [CompareAndDeleter] for Ack/Nack:
// "the unconditional delete removes the NEW claimant's entry: the job is
// then on no list, invisible to Reclaim, and silently lost". Reclaim's own
// trailing delete is the same unfenced shape.
// Surfaces: battery/queue/redis.go::RedisQueue.Reclaim — LPush (redis.go:512)
// then an unconditional HDel whose error is discarded (redis.go:515), while
// the fenced deleter releaseClaim (redis.go:362) exists in the same file.
// Finding: Reclaim's HGetAll snapshot observes W1's expired entry E1; after
// its re-enqueue push and before its HDel, W2 Dequeues the re-pushed job and
// records a fresh entry E2; the HDel deletes E2. W2 then crashes → the job
// is on NO list; the next Reclaim returns 0 — silent loss.
// Fix direction: drop the stale entry through the same fenced path Ack/Nack
// use (releaseClaim / CompareAndDeleter on the bytes the sweep snapshotted)
// so a newer claim's entry survives the sweep.

// redInterleaveLPush wraps mockRedis and, on its FIRST LPush after arming,
// lets the push land and then fires a scripted callback — the deterministic
// seam standing in for "another worker dequeued the just-re-pushed job
// before the sweeper's delete". The hook disarms itself before running, so
// pushes issued inside the script pass through untouched.
type redInterleaveLPush struct {
	*mockRedis
	before func()
}

func (f *redInterleaveLPush) LPush(ctx context.Context, key string, values ...any) error {
	if f.before == nil {
		return f.mockRedis.LPush(ctx, key, values...)
	}
	hook := f.before
	f.before = nil
	if err := f.mockRedis.LPush(ctx, key, values...); err != nil {
		return err
	}
	hook()
	return nil
}

// TestReclaimRedDoesNotDeleteNewerClaim: W1's lease expires; Reclaim
// re-enqueues the job and, between that push and its trailing HDel, W2
// re-claims it with a fresh token. The HDel must not delete W2's processing
// entry — and the job must stay recoverable when W2 then crashes.
func TestReclaimRedDoesNotDeleteNewerClaim(t *testing.T) {
	r := newMockRedis()
	gate := &redInterleaveLPush{mockRedis: r}
	q := NewRedisQueue(gate, "jobs")
	now := time.Now()
	q.now = func() time.Time { return now }
	ctx := context.Background()
	q.SetVisibilityTimeout(time.Minute)

	if err := q.Enqueue(ctx, Job{ID: "j", MaxAttempts: 5}); err != nil {
		t.Fatalf("setup broken: enqueue: %v", err)
	}
	w1, err := q.Dequeue(ctx)
	if err != nil || w1.ID != "j" {
		t.Fatalf("setup broken: W1 dequeue: %+v (%v)", w1, err)
	}

	// The lease expires under the sweeper; arm the seam so W2 claims
	// between Reclaim's re-enqueue push and its trailing HDel.
	now = now.Add(2 * time.Minute)
	w2 := Job{}
	gate.before = func() {
		claimed, derr := q.Dequeue(ctx)
		if derr != nil {
			t.Fatalf("setup broken: interleave re-dequeue: %v", derr)
		}
		w2 = claimed
	}
	if _, err := q.Reclaim(ctx); err != nil {
		t.Fatalf("setup broken: reclaim: %v", err)
	}
	if w2.ClaimToken == "" {
		t.Fatalf("setup broken: the interleave never ran: no re-claim happened, so this asserts nothing")
	}
	if w2.ClaimToken == w1.ClaimToken {
		t.Fatalf("setup broken: re-claim minted the same token — W2 is not a NEWER claimant")
	}

	raw, err := r.HGet(ctx, "jobs:processing", "j")
	empty := errors.Is(err, ErrRedisEmpty)
	if err != nil && !empty {
		t.Fatalf("setup broken: read processing entry: %v", err)
	}
	if empty {
		t.Errorf("SECURITY: [queue-reclaim-fence] Reclaim's unconditional HDel (redis.go:515) deleted the NEWER claimant's processing entry for %q: W2 is in-flight with no record — a crash leaves the job on no list, invisible to the next Reclaim, while Reclaim reported success (silent loss). Ack/Nack fence this exact shape via releaseClaim; the sweep must too", "j")
	} else {
		var entry processingEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			t.Fatalf("setup broken: unmarshal processing entry: %v", err)
		}
		var cur Job
		if err := json.Unmarshal([]byte(entry.Job), &cur); err != nil {
			t.Fatalf("setup broken: unmarshal claimed job: %v", err)
		}
		if cur.ClaimToken != w2.ClaimToken {
			t.Errorf("SECURITY: [queue-reclaim-fence] processing entry token = %q, want the newer claim's %q: Reclaim mutated a claim other than the stale one it observed", cur.ClaimToken, w2.ClaimToken)
		}
	}

	// Impact arm: W2 crashes; the next sweep must still resurrect the job.
	now = now.Add(2 * time.Minute)
	n, err := q.Reclaim(ctx)
	if err != nil {
		t.Fatalf("setup broken: second reclaim: %v", err)
	}
	if n != 1 {
		t.Errorf("SECURITY: [queue-reclaim-fence] after W2's crash the next Reclaim recovered %d jobs, want 1 — the job is on no list and unrecoverable (silent loss)", n)
	}
}
