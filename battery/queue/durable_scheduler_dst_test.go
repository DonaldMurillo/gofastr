package queue

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func dstScheduler(t *testing.T, owner string) *DurableScheduler {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
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
// ONCE that day, at the first occurrence (01:30 EDT, 05:30 UTC), like
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
	// shows 01:30 again, but the schedule already ran today, no second
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
// (03:00 EDT, 07:00 UTC), like vixie cron, instead of silently skipping
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

// runFallBackSteps registers spec in zone, then heartbeats at each instant
// in steps, draining between them, and returns how many jobs each heartbeat
// enqueued.
func runFallBackSteps(t *testing.T, zone, spec string, base time.Time, steps ...time.Time) []int {
	t.Helper()
	s := dstScheduler(t, "fallback-steps")
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Cron("night", spec).Job("job", nil).RegisterAt(base.In(loc)); err != nil {
		t.Fatal(err)
	}
	counts := make([]int, 0, len(steps))
	for _, at := range steps {
		if err := s.RunOnce(context.Background(), at); err != nil {
			t.Fatalf("RunOnce at %s: %v", at, err)
		}
		counts = append(counts, len(pendingJobs(t, s.queue)))
		drainPending(t, s.queue)
	}
	return counts
}

// Two firing minutes in the repeated hour: "0,30 1" fires at 01:00 EDT and
// 01:30 EDT, and the repeat of 01:00 (EST) must not fire. The heartbeat
// after the 01:30 fire used to remember only 01:30 and fire 01:00 again.
func TestCronFallBackTwoMinutesStepwise(t *testing.T) {
	base := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
	got := runFallBackSteps(t, "America/New_York", "0,30 1 * * *", base,
		time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC),  // 01:00 EDT
		time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), // 01:30 EDT
		time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC),  // 01:00 EST, repeat
		time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), // 01:30 EST, repeat
		time.Date(2026, 11, 2, 6, 0, 0, 0, time.UTC),  // next day, 01:00 EST
	)
	if want := []int{1, 1, 0, 0, 1}; !slices.Equal(got, want) {
		t.Fatalf("jobs per heartbeat = %v, want %v", got, want)
	}
}

// One heartbeat after the whole night records both fires, not four. The
// catch-up enqueues the newest and marks the older one missed.
func TestCronFallBackTwoMinutesCatchUp(t *testing.T) {
	s := dstScheduler(t, "fallback-catchup")
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	if err := s.Cron("night", "0,30 1 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(context.Background(), time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := occurrenceStatuses(t, s.queue)
	if got["enqueued"] != 1 || got["skipped"] != 1 || len(got) != 2 {
		t.Fatalf("occurrences = %v, want 1 enqueued + 1 skipped", got)
	}
}

// Lord Howe falls back by 30 minutes (02:00 +11 -> 01:30 +10:30) on
// 2026-04-05, so only 01:30-01:59 repeats and 02:00 right after it fires.
func TestCronFallBackHalfHourShift(t *testing.T) {
	base := time.Date(2026, 4, 4, 14, 10, 0, 0, time.UTC)
	got := runFallBackSteps(t, "Australia/Lord_Howe", "0,30,45 1,2 * * *", base,
		time.Date(2026, 4, 4, 14, 30, 0, 0, time.UTC), // 01:30 +11
		time.Date(2026, 4, 4, 14, 45, 0, 0, time.UTC), // 01:45 +11
		time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC),  // 01:30 +10:30, repeat
		time.Date(2026, 4, 4, 15, 15, 0, 0, time.UTC), // 01:45 +10:30, repeat
		time.Date(2026, 4, 4, 15, 30, 0, 0, time.UTC), // 02:00 +10:30
	)
	if want := []int{1, 1, 0, 0, 1}; !slices.Equal(got, want) {
		t.Fatalf("jobs per heartbeat = %v, want %v", got, want)
	}
}

// Lord Howe springs forward by 30 minutes (02:00 +10:30 -> 02:30 +11) on
// 2026-10-04: a 02:15 spec fires once, at the transition.
func TestCronSpringForwardHalfHourShift(t *testing.T) {
	base := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	got := runFallBackSteps(t, "Australia/Lord_Howe", "15 2 * * *", base,
		time.Date(2026, 10, 3, 15, 30, 0, 0, time.UTC), // 02:30 +11, transition
		time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC),
	)
	if want := []int{1, 0}; !slices.Equal(got, want) {
		t.Fatalf("jobs per heartbeat = %v, want %v", got, want)
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
		if err := q.Ack(context.Background(), job); err != nil {
			t.Fatalf("ack drained job: %v", err)
		}
	}
}
