package queue

import (
	"context"
	"testing"
)

// ReplayJob says whether it queued anything, on every backend: true for a
// failed job, false for an unknown id, a pending job and a second replay.
func TestReplayJobReportsWhetherItQueued(t *testing.T) {
	_, db := openDBQueue(t, 0)
	mem := NewMemoryQueue(0)
	t.Cleanup(func() { _ = mem.Close() })
	backends := map[string]interface {
		Queue
		ReplayReporter
	}{"db": db, "memory": mem, "redis": NewRedisQueue(newMockRedis(), "jobs")}
	for name, q := range backends {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := q.Enqueue(ctx, Job{Type: "x", MaxAttempts: 1}); err != nil {
				t.Fatal(err)
			}
			job, err := q.Dequeue(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := q.ReplayJob(ctx, job.ID); got || err != nil {
				t.Fatalf("replay of a claimed job = %v, %v; want false", got, err)
			}
			if err := q.Nack(ctx, job); err != nil {
				t.Fatal(err)
			}
			if got, err := q.ReplayJob(ctx, "no-such-job"); got || err != nil {
				t.Fatalf("replay of an unknown id = %v, %v; want false", got, err)
			}
			if got, err := q.ReplayJob(ctx, job.ID); !got || err != nil {
				t.Fatalf("replay of a failed job = %v, %v; want true", got, err)
			}
			if got, err := q.ReplayJob(ctx, job.ID); got || err != nil {
				t.Fatalf("second replay = %v, %v; want false", got, err)
			}
			again, err := q.Dequeue(ctx)
			if err != nil || again.ID != job.ID {
				t.Fatalf("replayed job not dequeued: %v %v", again.ID, err)
			}
		})
	}
}
