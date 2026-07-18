package queue

import (
	"context"
	"testing"
	"time"
)

func TestDurableSchedulerSkipsTickWhilePriorOccurrenceIsActive(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "replica-a", LeaseDuration: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Every("digest", time.Minute).Job("digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	if err := sched.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	first := pendingJobs(t, q)
	if len(first) != 1 {
		t.Fatalf("first tick enqueued %d jobs, want 1", len(first))
	}

	if err := sched.RunOnce(context.Background(), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("overlapping tick grew pending jobs to %d, want 1", len(jobs))
	}
	var reason string
	if err := db.QueryRow("SELECT skip_reason FROM "+q.schedulerOccurrencesTable()+
		" WHERE schedule_id=$1 AND status='skipped'", "digest").Scan(&reason); err != nil {
		t.Fatalf("read overlap occurrence: %v", err)
	}
	if reason != "overlap" {
		t.Fatalf("skip reason = %q, want overlap", reason)
	}

	if err := q.Ack(context.Background(), first[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunOnce(context.Background(), base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("post-ack tick enqueued %d jobs, want 1", len(jobs))
	}
}
