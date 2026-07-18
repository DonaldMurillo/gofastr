package queue

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDurableSchedulerWatermarkCASUsesVersionNotTimestamps(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 123456789, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "replica-a", LeaseDuration: time.Minute,
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
	if err := sched.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("timestamp normalization stalled watermark: pending jobs = %d, want 1", len(jobs))
	}
	var version int64
	if err := db.QueryRow("SELECT version FROM "+q.schedulerSchedulesTable()+
		" WHERE id=$1", "digest").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("watermark version = %d, want 1", version)
	}
}

func TestDurableSchedulerMigratesExistingSchedulesTableVersion(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (
		id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		interval_ns BIGINT NOT NULL DEFAULT 0,
		cron_spec TEXT NOT NULL DEFAULT '',
		next_run DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`, q.schedulerSchedulesTable())); err != nil {
		t.Fatal(err)
	}

	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatalf("upgrade scheduler schema: %v", err)
	}
	rows, err := db.Query("PRAGMA table_info(" + q.schedulerSchedulesTable() + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "version" {
			found = true
		}
	}
	if !found {
		t.Fatal("existing schedules table was not upgraded with version column")
	}
}

func TestDurableSchedulerRetentionPrunesOnlySafeOldOccurrences(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID:             "replica-a",
		LeaseDuration:       time.Minute,
		OccurrenceRetention: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range []Job{
		{ID: "live-job", Type: "test"},
		{ID: "done-job", Type: "test"},
	} {
		if err := q.Enqueue(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Ack(context.Background(), "done-job"); err != nil {
		t.Fatal(err)
	}

	insertOccurrence := func(id, status, jobID string, scheduledTick, created time.Time) {
		t.Helper()
		_, err := db.Exec(fmt.Sprintf(`INSERT INTO %s
			(occurrence_id, schedule_id, scheduled_tick, status, skip_reason,
			 claim_owner, claim_fence, created_at, enqueued_job_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			q.schedulerOccurrencesTable()),
			id, "digest", scheduledTick, status, "", "replica-a", 1, created, jobID)
		if err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-48 * time.Hour)
	insertOccurrence("old-skipped", "skipped", "", old, old)
	insertOccurrence("old-live", "enqueued", "live-job", old.Add(time.Minute), old)
	insertOccurrence("old-done", "enqueued", "done-job", old.Add(2*time.Minute), old)
	recent := now.Add(-time.Hour)
	insertOccurrence("recent-skipped", "skipped", "", recent, recent)

	if err := sched.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT occurrence_id FROM " + q.schedulerOccurrencesTable() + " ORDER BY occurrence_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	want := []string{"old-live", "recent-skipped"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("retained occurrences = %v, want %v", got, want)
	}
}

func TestDurableSchedulerCreatesScheduleJobIndex(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatal(err)
	}
	indexName := q.table + "_scheduler_occurrences_schedule_job_idx"
	var sqlText string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name=$1`,
		indexName).Scan(&sqlText); err != nil {
		t.Fatalf("missing %s: %v", indexName, err)
	}
}
