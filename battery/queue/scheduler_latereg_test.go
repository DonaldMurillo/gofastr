package queue

import (
	"testing"
	"testing/synctest"
	"time"
)

// The natural wiring is: start the scheduler, then register jobs. A job
// registered after Start must still fire, the old snapshot-once + return-
// on-empty loop dropped everything registered late.
func TestSchedulerFiresJobsRegisteredAfterStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &recordQueue{}
		sched := NewScheduler(q)

		ctx := t.Context()
		go sched.Start(ctx)

		// Register AFTER Start, and after the loop has already picked its first
		// (empty) tick interval.
		time.Sleep(30 * time.Millisecond)
		sched.Every(20*time.Millisecond).Job("late", nil).Register()

		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if q.count() > 0 {
				return // fired, the late registration was picked up
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("job registered after Start never fired, scheduler dropped late registrations")
	})
}

// nextWake must never return a value that would busy-loop or stall.
func TestSchedulerNextWakeBounds(t *testing.T) {
	sched := NewScheduler(&recordQueue{})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := sched.nextWake(now); got != time.Minute {
		t.Fatalf("empty scheduler wake = %v, want the 1m cap", got)
	}
	sched.Every(24*time.Hour).Job("slow", nil).RegisterAt(now)
	if got := sched.nextWake(now); got != time.Minute {
		t.Fatalf("a far next run should wake at the 1m cap, got %v", got)
	}
	sched.Every(5*time.Second).Job("fast", nil).RegisterAt(now)
	if got := sched.nextWake(now); got != 5*time.Second {
		t.Fatalf("wake should be the earliest next run, got %v", got)
	}
	if got := sched.nextWake(now.Add(time.Hour)); got != minSchedulerWake {
		t.Fatalf("an overdue run should wake at the floor, got %v", got)
	}
	sched2 := NewScheduler(&recordQueue{})
	sched2.Every(0).Job("each-pass", nil).RegisterAt(now)
	if got := sched2.nextWake(now); got != time.Minute {
		t.Fatalf("Every(0) rides the 1m cap, got %v", got)
	}
}
