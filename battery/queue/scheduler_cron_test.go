package queue

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordQueue captures enqueued jobs for deterministic assertions.
type recordQueue struct {
	mu   sync.Mutex
	jobs []Job
}

func (r *recordQueue) Enqueue(_ context.Context, j Job) error {
	r.mu.Lock()
	r.jobs = append(r.jobs, j)
	r.mu.Unlock()
	return nil
}
func (r *recordQueue) Dequeue(_ context.Context, _ ...string) (Job, error) { return Job{}, ErrNoJob }
func (r *recordQueue) Ack(_ context.Context, _ string) error               { return nil }
func (r *recordQueue) Nack(_ context.Context, _ string) error              { return nil }
func (r *recordQueue) Close() error                                        { return nil }

func (r *recordQueue) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

// A Cron schedule must enqueue exactly when its next tick arrives — driven
// deterministically off a fixed base time, no wall-clock sleeps.
func TestSchedulerCronFiresAtNextTick(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)

	// Anchor "now" so NextRun is computed from a known base.
	base := time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)
	if err := sched.Cron("0 2 * * *").Job("nightly", nil).RegisterAt(base); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}

	// Before 02:00 — must not fire.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 8, 1, 59, 0, 0, time.UTC))
	if got := rq.count(); got != 0 {
		t.Fatalf("fired early: got %d enqueues, want 0", got)
	}

	// At 02:00 — must fire exactly once.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC))
	if got := rq.count(); got != 1 {
		t.Fatalf("at next tick: got %d enqueues, want 1", got)
	}

	// Same day, after fire — must not double-fire; next run rolled to tomorrow.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 8, 23, 0, 0, 0, time.UTC))
	if got := rq.count(); got != 1 {
		t.Fatalf("double fired: got %d enqueues, want 1", got)
	}

	// Tomorrow 02:00 — fires again.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 9, 2, 0, 0, 0, time.UTC))
	if got := rq.count(); got != 2 {
		t.Fatalf("next day: got %d enqueues, want 2", got)
	}
}

// An invalid cron spec is reported at registration time, not silently dropped.
func TestSchedulerCronRejectsBadSpec(t *testing.T) {
	sched := NewScheduler(&recordQueue{})
	if err := sched.Cron("not a cron").Job("x", nil).Register(); err == nil {
		t.Fatal("expected error for bad cron spec, got nil")
	}
}

// Interval-based schedules keep working exactly as before alongside cron ones.
func TestSchedulerEveryStillWorks(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)
	sched.Every(time.Hour).Job("hourly", nil).Register()

	// dispatchDue with a now past NextRun must fire the interval job.
	future := time.Now().Add(2 * time.Hour)
	sched.dispatchDue(context.Background(), future)
	if got := rq.count(); got != 1 {
		t.Fatalf("interval job: got %d enqueues, want 1", got)
	}
}
