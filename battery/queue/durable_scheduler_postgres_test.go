package queue

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func TestPG_DurableSchedulerVersionCASIgnoresTimestampRoundTrip(t *testing.T) {
	db := pgtest.DB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 123456789, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "postgres-a", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Every("digest", time.Minute).Job("digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	sched.beforeOccurrenceCommit = func() {
		if _, err := db.Exec("UPDATE "+q.schedulerSchedulesTable()+
			" SET updated_at=$1 WHERE id=$2", base.Truncate(time.Second), "digest"); err != nil {
			t.Errorf("normalize updated_at: %v", err)
		}
	}
	// PostgreSQL stores timestamps at microsecond precision, so evaluate one
	// microsecond after the nominal tick. The assertion is about the watermark
	// CAS surviving timestamp normalization, not sub-microsecond wake timing.
	if err := sched.RunOnce(context.Background(), base.Add(time.Minute+time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		var version int64
		var nextRun time.Time
		_ = db.QueryRow("SELECT version, next_run FROM "+q.schedulerSchedulesTable()+" WHERE id=$1", "digest").
			Scan(&version, &nextRun)
		var occurrences, queueRows int
		_ = db.QueryRow("SELECT COUNT(*) FROM " + q.schedulerOccurrencesTable()).Scan(&occurrences)
		_ = db.QueryRow("SELECT COUNT(*) FROM " + q.qt()).Scan(&queueRows)
		t.Fatalf("timestamp normalization stalled Postgres watermark: pending jobs=%d version=%d next_run=%s occurrences=%d queue_rows=%d",
			len(jobs), version, nextRun.Format(time.RFC3339Nano), occurrences, queueRows)
	}
}

// The tz column round-trips through Postgres: a cron schedule registered in
// a non-UTC location fires at its local wall-clock time.
func TestPG_CronNonUTCTick(t *testing.T) {
	db := pgtest.DB(t)
	q := newDurableTestQueue(t, db)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "postgres-tz", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	base := time.Date(2026, 1, 15, 1, 0, 0, 0, loc)
	if err := sched.Cron("daily-local", "0 2 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}

	tick := time.Date(2026, 1, 15, 2, 0, 0, 0, loc)
	if err := sched.RunOnce(context.Background(), tick.Add(time.Microsecond)); err != nil {
		t.Fatalf("RunOnce at local 02:00: %v", err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("enqueued %d jobs at local 02:00 tick, want 1", len(jobs))
	}
}
