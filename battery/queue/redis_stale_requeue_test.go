package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A requeue (Nack's retry push, Reclaim's re-delivery) is decided on a
// snapshot of the processing entry. When the claim changes between that
// read and the push, the push lands a second copy of the job on the main
// list: a third worker claims it, consumes the same attempt as the current
// claimant, and overwrites its processing entry. interleavePush fires a
// scripted callback just before the first push of a claimed job, the seam
// standing in for one round trip of skew (a GC pause, a Redis failover).
// Both push entry points are hooked: LPush for a client with no atomic
// capability, and MoveIfEqual for one that implements [CompareAndMover].
type interleavePush struct {
	*mockRedis
	before func()
}

func (f *interleavePush) fire() {
	if f.before != nil {
		hook := f.before
		f.before = nil
		hook()
	}
}

func (f *interleavePush) LPush(ctx context.Context, key string, values ...any) error {
	if key == "jobs" || key == "jobs:dead" {
		f.fire()
	}
	return f.mockRedis.LPush(ctx, key, values...)
}

func (f *interleavePush) MoveIfEqual(ctx context.Context, key, field, expect, list, value string) (bool, error) {
	f.fire()
	return f.mockRedis.MoveIfEqual(ctx, key, field, expect, list, value)
}

func newInterleavePushQueue(t *testing.T) (*RedisQueue, *interleavePush, *time.Time) {
	t.Helper()
	r := newMockRedis()
	gate := &interleavePush{mockRedis: r}
	q := NewRedisQueue(gate, "jobs")
	t.Cleanup(func() { _ = q.Close() })
	now := time.Now()
	q.now = func() time.Time { return now }
	q.SetVisibilityTimeout(time.Minute)
	if err := q.Enqueue(context.Background(), Job{ID: "pay", Type: "x", MaxAttempts: 2}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return q, gate, &now
}

// assertOneLiveCopy fails when the main list holds a copy of the job that
// is also claimed: a third claimant would get it.
func assertOneLiveCopy(t *testing.T, q *RedisQueue, gate *interleavePush, newer *Job) {
	t.Helper()
	if got, err := q.Dequeue(context.Background()); !errors.Is(err, ErrNoJob) {
		t.Fatalf("a third worker claimed %q at attempt %d (err %v) while the newer claim still runs it: the stale push left a second copy on the main list", got.ID, got.Attempts, err)
	}
	assertNewerClaimSurvives(t, gate.mockRedis, "pay", newer)
}

// W1's Nack passes its token check, then its lease expires, Reclaim
// re-delivers, and W2 re-claims before W1's retry push lands.
func TestStaleNackPushesNoSecondCopy(t *testing.T) {
	q, gate, now := newInterleavePushQueue(t)
	ctx := context.Background()
	w1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("W1 dequeue: %v", err)
	}
	w2 := &Job{}
	gate.before = func() {
		*now = now.Add(2 * time.Minute)
		if _, err := q.Reclaim(ctx); err != nil {
			t.Fatalf("interleave reclaim: %v", err)
		}
		if *w2, err = q.Dequeue(ctx); err != nil {
			t.Fatalf("interleave re-dequeue: %v", err)
		}
	}
	if err := q.Nack(ctx, w1); err != nil {
		t.Fatalf("stale nack: %v", err)
	}
	assertOneLiveCopy(t, q, gate, w2)
}

// Reclaim reads W1's expired entry, then W1's own Nack lands and W2
// re-claims before Reclaim's re-delivery push.
func TestStaleReclaimPushesNoSecondCopy(t *testing.T) {
	q, gate, now := newInterleavePushQueue(t)
	ctx := context.Background()
	w1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("W1 dequeue: %v", err)
	}
	*now = now.Add(2 * time.Minute)
	w2 := &Job{}
	gate.before = func() {
		if err := q.Nack(ctx, w1); err != nil {
			t.Fatalf("interleave nack: %v", err)
		}
		if *w2, err = q.Dequeue(ctx); err != nil {
			t.Fatalf("interleave re-dequeue: %v", err)
		}
	}
	if _, err := q.Reclaim(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	assertOneLiveCopy(t, q, gate, w2)
}
