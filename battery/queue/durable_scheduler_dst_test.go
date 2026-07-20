package queue

import (
	"context"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func dstScheduler(t *testing.T, owner string) *DurableScheduler {
	t.Helper()
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: owner, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Fall-back: America/New_York repeats the 01:00–02:00 wall-clock hour on
// 2026-11-01 (02:00 EDT -> 01:00 EST). A "daily at 01:30" schedule fires
// ONCE that day — at the first occurrence (01:30 EDT, 05:30 UTC) — like
// vixie cron, not twice.
func TestCronFallBackFiresOnce(t *testing.T) {
	s := dstScheduler(t, "fallback")
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	if err := s.Cron("night", "30 1 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	// Heartbeat at the first 01:30 (EDT, 05:30 UTC): fires.
	firstHalfHour := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	if err := s.RunOnce(context.Background(), firstHalfHour); err != nil {
		t.Fatalf("RunOnce at 01:30 EDT: %v", err)
	}
	if jobs := pendingJobs(t, s.queue); len(jobs) != 1 {
		t.Fatalf("first 01:30 fired %d jobs, want 1", len(jobs))
	}
	drainPending(t, s.queue)

	// Heartbeat at the repeated 01:30 (EST, 06:30 UTC): the wall clock
	// shows 01:30 again, but the schedule already ran today — no second
	// fire (vixie parity).
	repeatedHalfHour := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	if err := s.RunOnce(context.Background(), repeatedHalfHour); err != nil {
		t.Fatalf("RunOnce at repeated 01:30 EST: %v", err)
	}
	if jobs := pendingJobs(t, s.queue); len(jobs) != 0 {
		t.Fatalf("repeated 01:30 fired %d extra jobs, want 0", len(jobs))
	}
}

// Spring-forward: America/New_York skips 02:00–03:00 on 2026-03-08. A
// "daily at 02:00" schedule fires ONCE that day, at the transition instant
// (03:00 EDT, 07:00 UTC) — like vixie cron — instead of silently skipping
// the whole day.
func TestCronSpringForwardFiresAtTransition(t *testing.T) {
	s := dstScheduler(t, "springfwd")
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 7, 12, 0, 0, 0, loc)
	if err := s.Cron("early", "0 2 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	// Sunday's 02:00 does not exist (clocks jump 02:00 EST -> 03:00 EDT);
	// the fire lands at the transition instant instead of skipping the day.
	if err := s.RunOnce(context.Background(), time.Date(2026, 3, 8, 8, 0, 0, 0, loc)); err != nil {
		t.Fatalf("RunOnce post-transition: %v", err)
	}
	jobs := pendingJobs(t, s.queue)
	if len(jobs) != 1 {
		t.Fatalf("spring-forward day enqueued %d jobs, want exactly 1 (fired at the transition)", len(jobs))
	}
}

// drainPending consumes every pending job so subsequent assertions count
// only newly enqueued work.
func drainPending(t *testing.T, q *DBQueue) {
	t.Helper()
	for {
		job, err := q.Dequeue(context.Background(), "job")
		if err != nil {
			return
		}
		if err := q.Ack(context.Background(), job.ID); err != nil {
			t.Fatalf("ack drained job: %v", err)
		}
	}
}
