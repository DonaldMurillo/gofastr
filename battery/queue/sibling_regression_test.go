package queue

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// A handler that edits its payload and fails must not change what the
// retry sees: the retry runs the job as it was enqueued.
func TestRetrySeesOriginalPayload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := NewMemoryQueue(1)
		defer q.Close()
		seen := make(chan string, 2)
		q.RegisterHandler("edit", func(ctx context.Context, job Job) error {
			seen <- string(job.Payload)
			if job.Attempts == 0 {
				job.Payload[0] = '2'
				return context.Canceled
			}
			return nil
		})
		q.Start()
		if err := q.Enqueue(context.Background(), Job{ID: "e", Type: "edit", Payload: []byte("1"), MaxAttempts: 2}); err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			select {
			case got := <-seen:
				if got != "1" {
					t.Fatalf("attempt %d saw payload %q", i, got)
				}
			case <-time.After(time.Minute):
				t.Fatal("handler did not run")
			}
		}
	})
}

// Replay removes the dead record without leaving its payload reachable
// past the slice's length.
func TestReplayClearsDeadTail(t *testing.T) {
	q := NewMemoryQueue(0)
	defer q.Close()
	q.retainDead(Job{ID: "d", Type: "x", Payload: []byte("secret")})
	if err := q.Replay(context.Background(), "d"); err != nil {
		t.Fatal(err)
	}
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	if tail := q.dead[:cap(q.dead)]; len(tail) > 0 && tail[0].job.Payload != nil {
		t.Fatal("replayed dead job still referenced past len")
	}
}

// A replayed job that dies again before Replay retires the record it
// replayed keeps its new dead record: Replay removes the record it took,
// not whatever record holds the ID by then.
func TestReplayKeepsRefailedDeadRecord(t *testing.T) {
	q := NewMemoryQueue(0)
	defer q.Close()
	ctx := context.Background()
	q.retainDead(Job{ID: "d", Type: "x", MaxAttempts: 1})
	refailed := false
	q.replayEnqueued = func() {
		job, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("dequeue replayed job: %v", err)
		}
		// MaxAttempts 1: this Nack exhausts the job and retains it dead.
		if err := q.Nack(ctx, job); err != nil {
			t.Fatalf("nack replayed job: %v", err)
		}
		refailed = true
	}
	if err := q.Replay(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	if !refailed {
		t.Fatal("the seam never ran, so this asserts nothing")
	}
	jobs, err := q.ListJobs(ctx, "failed", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "d" || jobs[0].Attempts != 1 {
		t.Fatalf("failed jobs = %+v; want the re-failed job: Replay deleted its new dead record and the job is in no queue and no dead list", jobs)
	}
}

// "all" lists every job on the DB backend too, as on memory and Redis.
func TestDBListJobsAll(t *testing.T) {
	_, q := openDBQueue(t, 0)
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "p", Type: "x", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	jobs, err := q.ListJobs(ctx, "all", 10, 0)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("ListJobs(all) = %d jobs, %v; want 1", len(jobs), err)
	}
}
