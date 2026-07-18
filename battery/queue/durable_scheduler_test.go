package queue

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func openDurableSchedulerDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "scheduler.db")) +
		"?_busy_timeout=5000&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newDurableTestQueue(t *testing.T, db *sql.DB) *DBQueue {
	t.Helper()
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("NewDBQueue: %v", err)
	}
	return q
}

func pendingJobs(t *testing.T, q *DBQueue) []Job {
	t.Helper()
	jobs, err := q.ListJobs(context.Background(), "pending", 100)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	return jobs
}

func occurrenceStatuses(t *testing.T, q *DBQueue) map[string]int {
	t.Helper()
	rows, err := q.db.Query("SELECT status, COUNT(*) FROM " +
		q.schedulerOccurrencesTable() + " GROUP BY status")
	if err != nil {
		t.Fatalf("query occurrences: %v", err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan occurrences: %v", err)
		}
		got[status] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("occurrence rows: %v", err)
	}
	return got
}

func TestDurableSchedulerReplicasEnqueueOneOccurrence(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q1 := newDurableTestQueue(t, db)
	q2 := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	s1, err := NewDurableScheduler(q1, DurableSchedulerConfig{
		OwnerID: "replica-a", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewDurableScheduler(q2, DurableSchedulerConfig{
		OwnerID: "replica-b", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Every("digest", time.Minute).Job("send-digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, sched := range []*DurableScheduler{s1, s2} {
		wg.Add(1)
		go func(s *DurableScheduler) {
			defer wg.Done()
			errs <- s.RunOnce(context.Background(), base.Add(time.Minute))
		}(sched)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("RunOnce: %v", err)
		}
	}

	jobs := pendingJobs(t, q1)
	if len(jobs) != 1 {
		t.Fatalf("two replicas enqueued %d jobs, want exactly 1", len(jobs))
	}
	if jobs[0].OccurrenceID == "" {
		t.Fatal("scheduled job must carry a stable occurrence identity")
	}
	if got := occurrenceStatuses(t, q1)["enqueued"]; got != 1 {
		t.Fatalf("enqueued occurrences = %d, want 1", got)
	}
}

func TestDurableSchedulerFencesPausedClaimantAfterLeaseHandoff(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q1 := newDurableTestQueue(t, db)
	q2 := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	cfgA := DurableSchedulerConfig{OwnerID: "replica-a", LeaseDuration: 30 * time.Second}
	cfgB := DurableSchedulerConfig{OwnerID: "replica-b", LeaseDuration: 30 * time.Second}
	s1, _ := NewDurableScheduler(q1, cfgA)
	s2, _ := NewDurableScheduler(q2, cfgB)
	if err := s1.Every("retention", time.Minute).Job("retain", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	paused := make(chan struct{})
	resume := make(chan struct{})
	s1.beforeOccurrenceCommit = func() {
		close(paused)
		<-resume
	}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- s1.RunOnce(context.Background(), base.Add(time.Minute))
	}()
	<-paused

	// Replica B reclaims the expired lease and commits the occurrence while
	// A is partitioned between evaluation and its ownership re-check.
	if err := s2.RunOnce(context.Background(), base.Add(91*time.Second)); err != nil {
		t.Fatalf("handoff RunOnce: %v", err)
	}
	close(resume)
	if err := <-firstDone; err != nil {
		t.Fatalf("stale claimant RunOnce: %v", err)
	}

	if jobs := pendingJobs(t, q1); len(jobs) != 1 {
		t.Fatalf("lease handoff enqueued %d jobs, want exactly 1", len(jobs))
	}
	if got := occurrenceStatuses(t, q1)["enqueued"]; got != 1 {
		t.Fatalf("handoff enqueued occurrences = %d, want 1", got)
	}
}

func TestDurableSchedulerRestartResumesPersistedWatermark(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	first, _ := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "before-restart", LeaseDuration: 10 * time.Second,
	})
	if err := first.Every("backup", time.Minute).Job("backup", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	if err := first.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	firstJobs := pendingJobs(t, q)
	if len(firstJobs) != 1 {
		t.Fatalf("first process enqueued %d jobs, want 1", len(firstJobs))
	}
	firstOccurrenceID := firstJobs[0].OccurrenceID
	// Complete the first occurrence so the next tick is not an overlap skip.
	if err := q.Ack(context.Background(), firstJobs[0].ID); err != nil {
		t.Fatal(err)
	}

	restarted, _ := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "after-restart", LeaseDuration: 10 * time.Second,
	})
	// Re-registering updates the definition but must not reset next_run.
	if err := restarted.Every("backup", time.Minute).Job("backup", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	if err := restarted.RunOnce(context.Background(), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	jobs := pendingJobs(t, q)
	if len(jobs) != 1 {
		t.Fatalf("restart produced %d pending jobs, want the next persisted tick", len(jobs))
	}
	if jobs[0].OccurrenceID == firstOccurrenceID {
		t.Fatalf("restart duplicated occurrence identity %q", jobs[0].OccurrenceID)
	}
}

func TestDurableSchedulerRecordsMissedTicksInsteadOfBursting(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sched, _ := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "replica-a", LeaseDuration: time.Minute,
	})
	if err := sched.Every("digest", time.Minute).Job("digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	// Three ticks elapsed. Only the latest occurrence should enqueue; older
	// ticks are durable "skipped" history, not a catch-up burst.
	if err := sched.RunOnce(context.Background(), base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("late evaluation enqueued %d jobs, want 1", len(jobs))
	}
	statuses := occurrenceStatuses(t, q)
	if statuses["skipped"] != 2 || statuses["enqueued"] != 1 {
		t.Fatalf("occurrence statuses = %#v, want skipped=2 enqueued=1", statuses)
	}
}
