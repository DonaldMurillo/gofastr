package queue

import (
	"context"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestDurableSchedulerReregisterFencesStaleDefinition(t *testing.T) {
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
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "pure-sqlite", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	if err := scheduler.Every("digest", time.Minute).Job("old-definition", nil).RegisterAt(base); err != nil {
		t.Fatalf("register old definition: %v", err)
	}

	scheduler.beforeOccurrenceCommit = func() {
		scheduler.beforeOccurrenceCommit = nil
		if err := scheduler.Every("digest", time.Minute).Job("new-definition", nil).RegisterAt(base); err != nil {
			t.Fatalf("re-register schedule: %v", err)
		}
	}
	due := base.Add(time.Minute)
	if err := scheduler.RunOnce(context.Background(), due); err != nil {
		t.Fatalf("run stale evaluator: %v", err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 0 {
		t.Fatalf("stale definition enqueued %d jobs, want 0", len(jobs))
	}

	if err := scheduler.RunOnce(context.Background(), due); err != nil {
		t.Fatalf("run current evaluator: %v", err)
	}
	jobs := pendingJobs(t, q)
	if len(jobs) != 1 {
		t.Fatalf("current definition enqueued %d jobs, want 1", len(jobs))
	}
	if jobs[0].Type != "new-definition" {
		t.Fatalf("enqueued job type = %q, want new-definition", jobs[0].Type)
	}
}
