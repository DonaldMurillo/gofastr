package queue

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// openHistoryQueue is a SQLite DBQueue with the given options and no
// workers; the test drives claims and outcomes itself.
func openHistoryQueue(t *testing.T, opts ...DBQueueOption) (*sql.DB, *DBQueue) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	q, err := NewDBQueue(db, append([]DBQueueOption{WithWorkers(0)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return db, q
}

func onlyJob(t *testing.T, q *DBQueue, status string) Job {
	t.Helper()
	jobs, err := q.ListJobs(context.Background(), status, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListJobs(%q) = %d jobs, want 1", status, len(jobs))
	}
	return jobs[0]
}

// A listed job carries its status and when it last changed; a failed
// attempt leaves its error, scrubbed of control bytes and cut short.
func TestDBListJobsCarriesStatusUpdatedAndLastError(t *testing.T) {
	_, q := openHistoryQueue(t)
	ctx := context.Background()
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return clock }
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "mail", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	if j := onlyJob(t, q, "pending"); j.Status != "pending" || !j.UpdatedAt.Equal(clock) {
		t.Fatalf("pending job = %+v", j)
	}
	clock = clock.Add(time.Minute)
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if j := onlyJob(t, q, "claimed"); j.Status != "claimed" || !j.UpdatedAt.Equal(clock) {
		t.Fatalf("claimed job = %+v", j)
	}
	clock = clock.Add(time.Minute)
	reason := errors.New("smtp: 451\x1b[31m greylisted " + strings.Repeat("x", 2000))
	if err := q.fail(ctx, job, reason); err != nil {
		t.Fatal(err)
	}
	j := onlyJob(t, q, "failed")
	if j.Status != "failed" || !j.UpdatedAt.Equal(clock) {
		t.Fatalf("failed job = %+v", j)
	}
	if !strings.HasPrefix(j.LastError, "smtp: 451") || strings.ContainsRune(j.LastError, '\x1b') {
		t.Errorf("last error %q", j.LastError)
	}
	if n := len([]rune(j.LastError)); n > maxLastError {
		t.Errorf("last error is %d runes, cap %d", n, maxLastError)
	}
	// A replay keeps the reason it last failed for.
	if err := q.Replay(ctx, "j1"); err != nil {
		t.Fatal(err)
	}
	if j := onlyJob(t, q, "pending"); j.LastError == "" {
		t.Errorf("replay dropped the last error")
	}
}

// Without retention an acked job is deleted, as before; with it the job
// stays as done until the retention passes, then the next claim sweeps
// it.
func TestDBDoneRetention(t *testing.T) {
	ctx := context.Background()
	_, plain := openHistoryQueue(t)
	if err := plain.Enqueue(ctx, Job{ID: "a", Type: "t"}); err != nil {
		t.Fatal(err)
	}
	job, _ := plain.Dequeue(ctx)
	if err := plain.Ack(ctx, job); err != nil {
		t.Fatal(err)
	}
	if jobs, _ := plain.ListJobs(ctx, "", 10, 0); len(jobs) != 0 {
		t.Fatalf("an acked job stayed without retention: %+v", jobs)
	}

	_, q := openHistoryQueue(t, WithDoneRetention(time.Hour))
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return clock }
	if err := q.Enqueue(ctx, Job{ID: "b", Type: "t"}); err != nil {
		t.Fatal(err)
	}
	job, _ = q.Dequeue(ctx)
	clock = clock.Add(time.Second)
	if err := q.Ack(ctx, job); err != nil {
		t.Fatal(err)
	}
	if j := onlyJob(t, q, "done"); !j.UpdatedAt.Equal(clock) {
		t.Fatalf("done job = %+v", j)
	}
	if stats, _ := q.Stats(ctx); stats["done"] != 1 {
		t.Errorf("stats = %v", stats)
	}
	// A done job is never claimed again.
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("a done job was claimed: %v", err)
	}
	clock = clock.Add(2 * time.Hour)
	_, _ = q.Dequeue(ctx)
	if jobs, _ := q.ListJobs(ctx, "", 10, 0); len(jobs) != 0 {
		t.Fatalf("a done job outlived its retention: %+v", jobs)
	}
}

// The worker records why a handler failed.
func TestDBWorkerRecordsLastError(t *testing.T) {
	_, q := openHistoryQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.RegisterHandler("mail", func(context.Context, Job) error { return errors.New("webhook: 502 from upstream") })
	if err := q.Enqueue(ctx, Job{ID: "w1", Type: "mail", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	q.workers = 1
	q.Start(ctx)
	defer q.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if jobs, _ := q.ListJobs(ctx, "failed", 1, 0); len(jobs) == 1 {
			if jobs[0].LastError != "webhook: 502 from upstream" {
				t.Fatalf("last error %q", jobs[0].LastError)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the job never failed")
}
