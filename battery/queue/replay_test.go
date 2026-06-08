package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// compile-time: DBQueue is the durable backend that supports replay.
var _ Replayable = (*DBQueue)(nil)

// TestDBQueue_ReplayFailedJob pins the dead-letter replay path: a job that
// exhausted its attempts (status 'failed') can be reset to pending and
// dequeued again.
func TestDBQueue_ReplayFailedJob(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "x", Payload: json.RawMessage(`{}`), MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx) // attempts -> 1, claimed
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, job.ID); err != nil { // attempts >= max -> failed
		t.Fatal(err)
	}
	// Failed jobs are not dequeuable.
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("failed job should not be dequeuable, got %v", err)
	}

	if err := q.Replay(ctx, job.ID); err != nil {
		t.Fatalf("replay: %v", err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue after replay: %v", err)
	}
	if got.ID != job.ID {
		t.Fatalf("dequeued %s, want replayed %s", got.ID, job.ID)
	}
}

// TestDBQueue_ReplayUnknownIsNoop confirms Replay is idempotent/safe on an
// id that isn't a failed job (no error, no side effect).
func TestDBQueue_ReplayUnknownIsNoop(t *testing.T) {
	_, q := openDBQueue(t, 0)
	if err := q.Replay(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("replay unknown id should be a no-op, got %v", err)
	}
}

// TestDBQueue_ReplayPendingIsNoop confirms replaying a still-pending job
// doesn't duplicate or reset it into a bad state — it stays dequeuable once.
func TestDBQueue_ReplayPendingIsNoop(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "x", Payload: json.RawMessage(`{}`), MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	jobs, err := q.ListJobs(ctx, "pending", 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("listjobs pending: %v len=%d", err, len(jobs))
	}
	if err := q.Replay(ctx, jobs[0].ID); err != nil {
		t.Fatalf("replay pending: %v", err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("pending job should still dequeue once: %v", err)
	}
}
