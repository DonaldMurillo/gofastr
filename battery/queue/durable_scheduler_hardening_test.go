package queue

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
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

// TestDurableSchedulerHookPanicLogsAndCommits: the partition hook
// fires on the scheduler loop, which has no per-request net, so a
// panicking hook must be logged and swallowed while the occurrence
// commit continues — the recover guard in hookBeforeOccurrenceCommit
// is what keeps a poison hook from wedging every schedule.
func TestDurableSchedulerHookPanicLogsAndCommits(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "hook-panic", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Every("digest", time.Minute).Job("send-digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	sched.beforeOccurrenceCommit = func() { panic("hook boom") }

	if err := sched.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("RunOnce must continue past a panicking hook: %v", err)
	}
	if jobs := pendingJobs(t, q); len(jobs) != 1 {
		t.Fatalf("occurrence commit did not continue: pending jobs = %d, want 1", len(jobs))
	}
	if got := occurrenceStatuses(t, q)["enqueued"]; got != 1 {
		t.Fatalf("enqueued occurrences = %d, want 1", got)
	}
	if !strings.Contains(buf.String(), "hook boom") {
		t.Fatalf("panicking hook not logged; log = %q", buf.String())
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
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows: %v", err)
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
	// done-job is completed by a worker the way production does it,
	// claimed (Dequeue) then Acked, so its row is gone and retention may
	// prune its occurrence. Ack alone must not remove a never-claimed
	// pending row (parity with Redis/Memory, where Ack of nothing claimed
	// is a no-op).
	if err := q.Enqueue(context.Background(), Job{ID: "done-job", Type: "test"}); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Dequeue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != "done-job" {
		t.Fatalf("claimed %q, want done-job", claimed.ID)
	}
	if err := q.Ack(context.Background(), claimed); err != nil {
		t.Fatal(err)
	}
	// live-job stays enqueued and unclaimed.
	if err := q.Enqueue(context.Background(), Job{ID: "live-job", Type: "test"}); err != nil {
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
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows: %v", err)
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
