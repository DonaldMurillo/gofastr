package queue

import (
	"context"
	"testing"
	"time"
)

// The natural wiring is: start the scheduler, then register jobs. A job
// registered after Start must still fire — the old snapshot-once + return-
// on-empty loop dropped everything registered late.
func TestSchedulerFiresJobsRegisteredAfterStart(t *testing.T) {
	q := &recordQueue{}
	sched := NewScheduler(q)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	// Register AFTER Start, and after the loop has already picked its first
	// (empty) tick interval.
	time.Sleep(30 * time.Millisecond)
	sched.Every(20*time.Millisecond).Job("late", nil).Register()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if q.count() > 0 {
			return // fired — the late registration was picked up
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job registered after Start never fired — scheduler dropped late registrations")
}

// tickInterval must never return a value that would busy-loop or stall.
func TestSchedulerTickIntervalBounds(t *testing.T) {
	sched := NewScheduler(&recordQueue{})
	if got := sched.tickInterval(); got != time.Minute {
		t.Fatalf("empty scheduler tick = %v, want 1m default", got)
	}
	sched.Every(5*time.Second).Job("fast", nil).Register()
	if got := sched.tickInterval(); got != 5*time.Second {
		t.Fatalf("tick should adapt to the finest interval, got %v", got)
	}
	sched.Every(24*time.Hour).Job("slow", nil).Register()
	if got := sched.tickInterval(); got != 5*time.Second {
		t.Fatalf("finest interval should still win, got %v", got)
	}
}
