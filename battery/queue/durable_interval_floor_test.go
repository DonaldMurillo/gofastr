package queue

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// Ticks closer than a microsecond collapse to one Postgres timestamp, and
// the second insert broke UNIQUE(schedule_id, scheduled_tick), which the
// insert's ON CONFLICT(occurrence_id) does not cover: the pass returned
// before every later schedule, on every heartbeat (B44). A stored
// sub-millisecond interval is now skipped and the others fire.
func TestPG_TinyIntervalSkipsNotWedges(t *testing.T) {
	db := pgtest.DB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "tiny", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Every("a-tiny", time.Second).Job("tiny", nil).RegisterAt(base.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := sched.Every("digest", time.Minute).Job("digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	// The row an older build accepted: a one-nanosecond interval.
	if _, err := db.Exec("UPDATE "+q.schedulerSchedulesTable()+" SET interval_ns=1 WHERE id=$1", "a-tiny"); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunOnce(context.Background(), base.Add(time.Minute+time.Microsecond)); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	var digest int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+q.qt()+" WHERE type=$1", "digest").Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest != 1 {
		t.Fatalf("digest jobs = %d, want 1: the tiny schedule wedged the pass", digest)
	}
}

// Registration refuses a durable interval below a millisecond.
func TestDurableIntervalFloor(t *testing.T) {
	s := dstScheduler(t, "floor")
	for _, d := range []time.Duration{time.Nanosecond, time.Microsecond, time.Millisecond - 1} {
		if err := s.Every("tiny", d).Job("tiny", nil).Register(); err == nil {
			t.Errorf("Every(%s) registered", d)
		}
	}
	if err := s.Every("ok", time.Millisecond).Job("ok", nil).Register(); err != nil {
		t.Fatalf("Every(1ms): %v", err)
	}
}
