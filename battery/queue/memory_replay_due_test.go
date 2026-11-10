package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A replayed job is due at once ("attempts counter cleared, scheduled
// immediately", Replayable; DBQueue.Replay writes scheduled_at = now).
// MemoryQueue wrote a handler's RetryAfter delay into the job before it
// checked MaxAge, so a job dead-lettered because the delay landed past its
// age kept a due time hours ahead, and Replay re-enqueued it as it was:
// the operator's replay did nothing until the stale delay ran out, and the
// job was dropped if the process stopped first.

// The whole path: RetryAfter(2h) on a job with MaxAge 1h dead-letters it,
// the retained job carries no future due time, and a replay runs it now.
func TestMemoryReplayAfterRetryAfterPastMaxAgeIsDue(t *testing.T) {
	q := NewMemoryQueue(1)
	t.Cleanup(func() { q.Close() })
	var runs atomic.Int32
	q.RegisterHandler("deliver", func(context.Context, Job) error {
		runs.Add(1)
		// The receiver answers 429 with Retry-After: 2h.
		return RetryAfter(2*time.Hour, errors.New("429"))
	})
	q.Start()
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "deliver", MaxAttempts: 25, MaxAge: time.Hour}); err != nil {
		t.Fatal(err)
	}
	var dead []Job
	waitFor(t, func() bool {
		dead, _ = q.ListJobs(ctx, "failed", 10, 0)
		return len(dead) == 1
	}, 5*time.Second, "j1 dead-lettered: its Retry-After lands past MaxAge")
	if got := runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times before the dead-letter, want 1", got)
	}
	if at := dead[0].ScheduledAt; at.After(time.Now()) {
		t.Fatalf("the dead job is scheduled %s ahead: the Retry-After of a retry that was refused was written to it",
			time.Until(at).Round(time.Minute))
	}

	// The operator replays the dead job from the admin page.
	if err := q.Replay(ctx, "j1"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	waitFor(t, func() bool { return runs.Load() == 2 }, 3*time.Second,
		"the replayed job to run: a replay is due now, not after the failed attempt's Retry-After")
}

// Replay itself sets the due time: a dead job that carries a future one,
// however it came by it, runs as soon as it is replayed.
func TestMemoryReplaySchedulesNow(t *testing.T) {
	q := NewMemoryQueue(1)
	t.Cleanup(func() { q.Close() })
	ran := make(chan struct{}, 1)
	q.RegisterHandler("deliver", func(context.Context, Job) error {
		ran <- struct{}{}
		return nil
	})
	q.Start()
	q.retainDead(Job{ID: "j1", Type: "deliver", MaxAttempts: 3, Attempts: 3,
		CreatedAt: time.Now(), ScheduledAt: time.Now().Add(2 * time.Hour)})

	if err := q.Replay(context.Background(), "j1"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("the replayed job did not run: Replay kept the dead job's future ScheduledAt")
	}
}
