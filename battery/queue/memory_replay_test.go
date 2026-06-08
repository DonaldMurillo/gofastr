package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// compile-time: MemoryQueue must satisfy both the inspection and replay
// capabilities so admin tooling can list and re-queue dead jobs.
var (
	_ Replayable = (*MemoryQueue)(nil)
	_ Browsable  = (*MemoryQueue)(nil)
)

// driveToFailure enqueues a job with MaxAttempts=1, dequeues it (so it is
// tracked in the in-flight set), then Nacks it — exhausting its single attempt
// and pushing it into the terminal dead-letter store. Returns the job ID.
func driveToFailure(t *testing.T, q *MemoryQueue) string {
	t.Helper()
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "x", Payload: json.RawMessage(`{}`), MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if err := q.Nack(ctx, job.ID); err != nil { // attempts -> 1 >= max -> terminal
		t.Fatalf("nack: %v", err)
	}
	return job.ID
}

// TestMemoryQueue_DeadJobRetained pins the fix: a job that exhausts its
// attempts must be retained as 'failed' (inspectable) rather than silently
// dropped.
func TestMemoryQueue_DeadJobRetained(t *testing.T) {
	q := NewMemoryQueue(1)
	defer q.Close()
	ctx := context.Background()

	id := driveToFailure(t, q)

	failed, err := q.ListJobs(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("listjobs: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != id {
		t.Fatalf("expected dead job %s retained, got %+v", id, failed)
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["failed"] != 1 {
		t.Fatalf("expected failed count 1, got %d (%+v)", stats["failed"], stats)
	}
}

// TestMemoryQueue_ReplayRequeues confirms a retained failed job can be replayed
// back onto the pending set and dequeued again with attempts reset.
func TestMemoryQueue_ReplayRequeues(t *testing.T) {
	q := NewMemoryQueue(1)
	defer q.Close()
	ctx := context.Background()

	id := driveToFailure(t, q)

	if err := q.Replay(ctx, id); err != nil {
		t.Fatalf("replay: %v", err)
	}

	// The job is gone from the dead-letter store after replay.
	failed, err := q.ListJobs(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("listjobs after replay: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expected no failed jobs after replay, got %+v", failed)
	}

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue after replay: %v", err)
	}
	if got.ID != id {
		t.Fatalf("dequeued %s, want replayed %s", got.ID, id)
	}
	if got.Attempts != 0 {
		t.Fatalf("expected attempts reset to 0, got %d", got.Attempts)
	}
}

// TestMemoryQueue_ReplayUnknownIsNoop confirms Replay is idempotent/safe on an
// id that isn't a retained failed job.
func TestMemoryQueue_ReplayUnknownIsNoop(t *testing.T) {
	q := NewMemoryQueue(1)
	defer q.Close()
	ctx := context.Background()

	if err := q.Replay(ctx, "does-not-exist"); err != nil {
		t.Fatalf("replay unknown id should be a no-op, got %v", err)
	}
	// And it must not have invented a pending job.
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("expected no job after replaying unknown id, got %v", err)
	}
}
