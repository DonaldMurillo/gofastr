package queue

import (
	"context"
	"errors"
	"testing"
)

// ============================================================================
// Property: Replay must never lose the terminal (failed) record when the
// re-enqueue fails — the enqueue-first/remove-second ordering that
// RedisQueue.Replay documents as the lossless rule ("a failure between
// the two ops leaves the job on the dead list (recoverable) rather than
// dropping it"). Surface: MemoryQueue.Replay, which inverts it — it
// deletes from q.dead BEFORE calling Enqueue, so a cancelled context
// (battery/admin passes r.Context(), a client disconnect or request
// deadline fires it) or a Replay racing Close loses the record
// permanently: the visible error invites a retry that is then the
// documented idempotent no-op against a vanished job.
//
// (The companion memory-surface finding from the same recon pass — the
// worker pool dropping unregistered-type jobs without a trace — is
// already pinned, RED, by TestUnknownTypeJobDropIsObservable/memory in
// queue_security_test.go; not duplicated here.)
// ============================================================================

// TestReplayKeepsJobWhenEnqueueFails: a failed re-enqueue must leave the
// job retrievable from ListJobs("failed") so the visible error can be
// retried. Today the record is consumed first and the loss is permanent.
func TestReplayKeepsJobWhenEnqueueFails(t *testing.T) {
	assertRetained := func(t *testing.T, q *MemoryQueue, id string, replayErr error) {
		t.Helper()
		dead, err := q.ListJobs(context.Background(), "failed", 0)
		if err != nil {
			t.Fatalf("list failed jobs: %v", err)
		}
		if len(dead) != 1 || dead[0].ID != id {
			t.Fatalf("Replay consumed the terminal record before the re-enqueue succeeded (replay err=%v, failed jobs=%v): the visible error invites a retry that is now the documented idempotent no-op against a vanished job — permanent loss", replayErr, dead)
		}
	}

	t.Run("cancelled context", func(t *testing.T) {
		q := NewMemoryQueue(0)
		q.retainDead(Job{ID: "dead-1", Type: "x", MaxAttempts: 1})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := q.Replay(ctx, "dead-1")
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("Replay with a cancelled context = %v, want context.Canceled surfaced", err)
		}
		assertRetained(t, q, "dead-1", err)
	})

	t.Run("closed queue", func(t *testing.T) {
		q := NewMemoryQueue(0)
		q.retainDead(Job{ID: "dead-2", Type: "x", MaxAttempts: 1})
		if err := q.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		err := q.Replay(context.Background(), "dead-2")
		if err == nil || !errors.Is(err, ErrQueueClosed) {
			t.Fatalf("Replay against a closed queue = %v, want ErrQueueClosed surfaced", err)
		}
		assertRetained(t, q, "dead-2", err)
	})
}
