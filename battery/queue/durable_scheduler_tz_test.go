package queue

import (
	"context"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func newTZScheduler(t *testing.T, owner string) *DurableScheduler {
	t.Helper()
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: owner, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	return s
}

// A cron schedule registered in a non-UTC location fires at its local time.
func TestCronNonUTCTick(t *testing.T) {
	s := newTZScheduler(t, "tz")
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	base := time.Date(2026, 1, 15, 1, 0, 0, 0, loc)
	if err := s.Cron("daily-local", "0 2 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}

	tick := time.Date(2026, 1, 15, 2, 0, 0, 0, loc)
	if err := s.RunOnce(context.Background(), tick); err != nil {
		t.Fatalf("RunOnce at local 02:00: %v", err)
	}
	jobs := pendingJobs(t, s.queue)
	if len(jobs) != 1 {
		t.Fatalf("enqueued %d jobs at local 02:00 tick, want 1", len(jobs))
	}
}

// Re-registering a schedule with a different cadence must not strand the
// stored watermark: the next valid occurrence of the NEW cadence fires,
// and evaluation at the old watermark is not an error.
func TestCadenceChangeKeepsFiring(t *testing.T) {
	s := newTZScheduler(t, "cadence")
	base := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if err := s.Every("switch", time.Minute).Job("job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register interval: %v", err)
	}
	if err := s.Cron("switch", "0 2 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatalf("re-register cron: %v", err)
	}

	// Old interval watermark (00:01) is not an occurrence of the new cron.
	if err := s.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("RunOnce at stale watermark: %v", err)
	}
	if jobs := pendingJobs(t, s.queue); len(jobs) != 0 {
		t.Fatalf("enqueued %d jobs before the new cadence is due, want 0", len(jobs))
	}

	// First real occurrence of the new cron cadence.
	if err := s.RunOnce(context.Background(), time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RunOnce at new cadence tick: %v", err)
	}
	jobs := pendingJobs(t, s.queue)
	if len(jobs) != 1 {
		t.Fatalf("enqueued %d jobs at the new cadence tick, want 1", len(jobs))
	}
}

// Register() (wall-clock registration) keeps the historical UTC cron
// semantics: it must not persist the process-local zone. "Local" resolves
// differently on every replica, and existing schedules registered before
// the tz column fired in UTC.
func TestRegisterKeepsUTCCron(t *testing.T) {
	s := newTZScheduler(t, "wall-clock")
	if err := s.Cron("nightly", "0 2 * * *").Job("job", nil).Register(); err != nil {
		t.Fatalf("register: %v", err)
	}
	var tz string
	if err := s.queue.db.QueryRow(
		"SELECT tz FROM " + s.queue.schedulerSchedulesTable() + " WHERE id='nightly'",
	).Scan(&tz); err != nil {
		t.Fatalf("select tz: %v", err)
	}
	if tz != "" {
		t.Fatalf("Register() persisted tz=%q, want \"\" (UTC evaluation)", tz)
	}
	if got := tzName(time.Local); got != "" {
		t.Fatalf("tzName(time.Local) = %q, want \"\" (replica-dependent zone)", got)
	}
}
