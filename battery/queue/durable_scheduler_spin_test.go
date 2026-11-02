package queue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// A schedule the pass skips (a corrupted row, a stored sub-millisecond
// interval, a cron spec that no longer parses) keeps its past next_run.
// nextWakeDelay chose the earliest next_run of all, that one, and floored
// the wait at 10ms: Start re-ran the pass a hundred times a second,
// writing the lease row and logging the skip at ERROR on every pass. The
// wake now ignores a schedule the pass just left due; it is retried at the
// heartbeat, while a healthy schedule still wakes the loop on time.
func TestSkippedScheduleDoesNotSpinStart(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = db.Close() })
		assertSkippedScheduleDoesNotSpin(t, db)
	})
	t.Run("postgres", func(t *testing.T) {
		assertSkippedScheduleDoesNotSpin(t, pgtest.DB(t))
	})
}

func assertSkippedScheduleDoesNotSpin(t *testing.T, db *sql.DB) {
	q := newDurableTestQueue(t, db)
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{OwnerID: "spin", LeaseDuration: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if err := s.Every("broken", time.Second).Job("broken", nil).RegisterAt(base.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.Every("healthy", time.Minute).Job("healthy", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	// The row an older build accepted: the pass skips it every time.
	if _, err := db.Exec("UPDATE "+q.schedulerSchedulesTable()+" SET interval_ns=1 WHERE id=$1", "broken"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := base.Add(30 * time.Second)
	if err := s.RunOnce(ctx, now); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	heartbeat := 10 * time.Second
	delay, err := s.nextWakeDelay(ctx, now, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if delay != heartbeat {
		t.Errorf("wake after a pass that skipped a due schedule: %s, want the %s heartbeat", delay, heartbeat)
	}
	// The healthy schedule, due before the heartbeat, still sets the wake.
	delay, err = s.nextWakeDelay(ctx, base.Add(55*time.Second), heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if delay != 5*time.Second {
		t.Errorf("wake before the healthy schedule's next run: %s, want 5s", delay)
	}
}
