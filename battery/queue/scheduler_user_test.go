package queue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A durable schedule's jobs carried no UserID, so the queue_jobs eraser
// matched none of them, and the schedule row kept the payload and went on
// firing jobs with the erased user's data (2026-10-27 S25). A schedule now
// takes UserID: its jobs carry it, and the erasure deletes the jobs and
// the schedule.
func TestScheduleUserIDReachesErasure(t *testing.T) {
	ctx := context.Background()
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{OwnerID: "erase"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if err := s.Every("digest-u1", time.Minute).Job("digest", map[string]string{"to": "u1@example.com"}).
		UserID("u1").RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	if err := s.Every("digest-u2", time.Minute).Job("digest", map[string]string{"to": "u2@example.com"}).
		UserID("u2").RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(ctx, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := columnValues(t, db, "SELECT user_id FROM "+q.qt()+" ORDER BY user_id"); len(got) != 2 || got[0] != "u1" || got[1] != "u2" {
		t.Fatalf("job owners = %v, want [u1 u2]", got)
	}

	if _, err := framework.NewApp(framework.WithDB(db)).EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	if got := columnValues(t, db, "SELECT user_id FROM "+q.qt()); len(got) != 1 || got[0] != "u2" {
		t.Errorf("job owners after erasing u1 = %v, want [u2]", got)
	}
	if got := columnValues(t, db, "SELECT id FROM "+q.schedulerSchedulesTable()); len(got) != 1 || got[0] != "digest-u2" {
		t.Errorf("schedules after erasing u1 = %v, want only digest-u2", got)
	}
}

// A schedules table created before user_id existed gains the column on the
// next NewDurableScheduler, and a schedule registered then keeps its user.
func TestOldScheduleTableGainsUserID(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	if _, err := db.Exec(`CREATE TABLE ` + q.schedulerSchedulesTable() + ` (
		id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		interval_ns BIGINT NOT NULL DEFAULT 0,
		cron_spec TEXT NOT NULL DEFAULT '',
		next_run DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{})
	if err != nil {
		t.Fatalf("upgrade scheduler schema: %v", err)
	}
	if err := s.Every("digest", time.Minute).Job("digest", nil).UserID("u1").
		RegisterAt(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if got := columnValues(t, db, "SELECT user_id FROM "+q.schedulerSchedulesTable()); len(got) != 1 || got[0] != "u1" {
		t.Errorf("schedule user_id = %v, want [u1]", got)
	}
}

// columnValues runs a one-column query and returns its rows.
func columnValues(t *testing.T, db *sql.DB, stmt string) []string {
	t.Helper()
	rows, err := db.Query(stmt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The in-memory Scheduler holds its schedules in process memory, but the
// jobs it fires land in a queue an erasure reaches, so they carry the
// schedule's UserID too.
func TestInMemoryScheduleCarriesUserID(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if err := sched.Every(time.Minute).Job("digest", nil).UserID("u1").RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	sched.dispatchDue(context.Background(), base.Add(time.Minute))
	jobs := rq.capturedJobs()
	if len(jobs) != 1 || jobs[0].UserID != "u1" {
		t.Fatalf("fired jobs = %+v, want one with UserID u1", jobs)
	}
}

// App.EraseUserData deletes the queue_jobs rows in its SQL plan and runs
// the in-memory Scheduler's eraser only after the commit, so a tick in
// between put the erased user's job back in queue_jobs (bug hunt r7
// R7-004). The scheduler's eraser now sweeps the DBQueues it feeds once
// its schedules are gone. The tick here stands in for one in that window.
func TestSchedulerEraseSweepsFedDBQueue(t *testing.T) {
	ctx := context.Background()
	db, q := openDBQueue(t, 0)
	sched := NewScheduler(q)
	t.Cleanup(func() { _ = sched.Close() })
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, u := range []string{"u1", "u2"} {
		if err := sched.Every(time.Minute).Job("digest", map[string]string{"to": u}).UserID(u).RegisterAt(base); err != nil {
			t.Fatal(err)
		}
	}
	sched.dispatchDue(ctx, base.Add(time.Minute))
	if _, err := sched.eraseUser(ctx, "u1", false); err != nil {
		t.Fatalf("eraseUser: %v", err)
	}
	var u1, u2 int
	if err := db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN user_id = 'u1' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN user_id = 'u2' THEN 1 ELSE 0 END), 0) FROM queue_jobs`).Scan(&u1, &u2); err != nil {
		t.Fatal(err)
	}
	if u1 != 0 || u2 != 1 {
		t.Fatalf("queue_jobs holds %d u1 and %d u2 jobs after erasing u1, want 0 and 1", u1, u2)
	}
}
