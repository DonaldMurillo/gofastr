package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestDurableSchedulerPureSQLiteVersionAndRetention(t *testing.T) {
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
	now := time.Date(2026, 7, 18, 12, 0, 0, 123456789, time.UTC)
	scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID:             "pure-sqlite",
		LeaseDuration:       time.Minute,
		OccurrenceRetention: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	if err := scheduler.Every("digest", time.Minute).Job("digest", map[string]any{"tenant": "one"}).RegisterAt(now); err != nil {
		t.Fatalf("register: %v", err)
	}

	scheduler.beforeOccurrenceCommit = func() {
		if _, err := db.Exec("UPDATE "+q.schedulerSchedulesTable()+
			" SET updated_at=$1 WHERE id=$2", now.Truncate(time.Second), "digest"); err != nil {
			t.Errorf("normalize timestamp: %v", err)
		}
	}
	if err := scheduler.RunOnce(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("run due schedule: %v", err)
	}

	var version, jobs int
	if err := db.QueryRow("SELECT version FROM "+q.schedulerSchedulesTable()+" WHERE id=$1", "digest").Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM " + q.qt()).Scan(&jobs); err != nil {
		t.Fatalf("count enqueued jobs: %v", err)
	}
	if jobs != 1 {
		t.Fatalf("enqueued jobs = %d, want 1", jobs)
	}

	old := now.Add(-48 * time.Hour)
	if _, err := db.Exec(fmt.Sprintf(`INSERT INTO %s
		(occurrence_id, schedule_id, scheduled_tick, status, skip_reason,
		 claim_owner, claim_fence, created_at, enqueued_job_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, q.schedulerOccurrencesTable()),
		"old-skipped", "digest", old, "skipped", "catch-up-cap",
		"pure-sqlite", 1, old, ""); err != nil {
		t.Fatalf("seed old occurrence: %v", err)
	}
	if err := scheduler.RunOnce(context.Background(), now.Add(time.Minute+defaultRetentionSweepPeriod)); err != nil {
		t.Fatalf("retention sweep: %v", err)
	}
	var oldOccurrences int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+q.schedulerOccurrencesTable()+
		" WHERE occurrence_id=$1", "old-skipped").Scan(&oldOccurrences); err != nil {
		t.Fatalf("count retained occurrence: %v", err)
	}
	if oldOccurrences != 0 {
		t.Fatalf("old skipped occurrences = %d, want 0", oldOccurrences)
	}
}
