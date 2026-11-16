package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The erasure deletes a user's jobs, then their schedules, then the jobs
// again: erasers run in name order, and the last delete is what catches a
// job a tick committed while the erasure waited on the schedule row.
func TestDurableSchedulerEraserOrder(t *testing.T) {
	q := newDurableTestQueue(t, openDurableSchedulerDB(t))
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, e := range datexport.AllErasers() {
		pos[e.Name] = i + 1
	}
	jobs := pos[datexport.ScopedName("queue_jobs", q.table)]
	schedules := pos[datexport.ScopedName("queue_scheduler_schedules", q.schedulerName("_scheduler_schedules"))]
	sweep := pos[datexport.ScopedName(schedulerJobSweepEraser, q.table)]
	if jobs == 0 || schedules == 0 || sweep == 0 {
		t.Fatalf("erasers registered: jobs %d, schedules %d, sweep %d; want all three", jobs, schedules, sweep)
	}
	if sweep < schedules {
		t.Fatalf("the job sweep runs at %d, before the schedules at %d", sweep, schedules)
	}
}

// On Postgres an erasure that ran while the scheduler committed a tick left
// the tick's job, user_id and payload, in queue_jobs. A blocker row holds the
// scheduler's job INSERT open so the interleaving is the same every run.
func TestDurableSchedulerEraseRacingATick(t *testing.T) {
	ctx := context.Background()
	db := pgtest.DB(t)
	db.SetMaxOpenConns(8)
	q, err := NewDBQueue(db, WithWorkers(0))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	if err := s.Every("digest-u1", time.Minute).Job("digest", json.RawMessage(`{"email":"u1@example.com"}`)).
		UserID("u1").RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)

	waiting := func(n int) {
		t.Helper()
		for range 400 {
			var c int
			if err := db.QueryRow(`SELECT count(*) FROM pg_stat_activity
				WHERE wait_event_type='Lock' AND datname=current_database()`).Scan(&c); err != nil {
				t.Fatal(err)
			}
			if c >= n {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("never saw %d lock waiters", n)
	}

	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.Exec(`INSERT INTO queue_jobs (id, type, created_at, scheduled_at) VALUES ($1,'x',now(),now())`,
		stableOccurrenceID("digest-u1", now)); err != nil {
		t.Fatal(err)
	}

	schedDone := make(chan error, 1)
	go func() { schedDone <- s.RunOnce(ctx, now) }()
	waiting(1) // the scheduler holds the schedule row and waits to insert the job

	eraseDone := make(chan error, 1)
	go func() {
		_, err := framework.NewApp(framework.WithDB(db)).EraseUserData(ctx, "u1")
		eraseDone <- err
	}()
	waiting(2) // the erasure deleted u1's jobs and waits on the schedule row

	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-schedDone; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if err := <-eraseDone; err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if n := countJobs(t, db, q.qt()); n != 0 {
		t.Fatalf("EraseUserData returned nil but %d of u1's jobs survive", n)
	}
}
