package queue

import (
	"context"
	"testing"
	"time"
)

// Occurrence retention kept an occurrence only while its job was pending
// or claimed, so it pruned one whose job had failed. Replay made that job
// pending again, but the overlap check reaches a schedule's jobs through
// its occurrences: with the occurrence gone it saw no active job, and the
// next tick enqueued a second run of the schedule beside the replayed one.
// Retention now keeps an occurrence while its job still exists in any
// state but done.
func TestReplayedJobStillBlocksOverlap(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	ctx := context.Background()
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "replica-a", LeaseDuration: 5 * time.Minute, OccurrenceRetention: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Every("digest", time.Minute).Job("digest", nil).MaxAttempts(1).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunOnce(ctx, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx, "digest")
	if err != nil {
		t.Fatalf("claim the first tick's job: %v", err)
	}
	if err := q.Nack(ctx, job); err != nil { // its only attempt: failed
		t.Fatal(err)
	}

	later := base.Add(3 * time.Hour)
	if err := sched.sweepOccurrences(ctx, later); err != nil {
		t.Fatal(err)
	}
	if err := q.Replay(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunOnce(ctx, later); err != nil {
		t.Fatal(err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("pending jobs after the tick = %d, want 1: the replayed job still runs, "+
			"so the tick is an overlap", len(jobs))
	}
}
