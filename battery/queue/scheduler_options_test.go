package queue

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

// capturedJobs returns a snapshot of the jobs a recordQueue has enqueued.
func (r *recordQueue) capturedJobs() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, len(r.jobs))
	copy(out, r.jobs)
	return out
}

// ============================================================================
// In-memory scheduler: Lane / Priority / MaxAttempts set on the builder are
// carried verbatim into the fired Job. Omitted options preserve the existing
// defaults (empty lane, priority 0, max attempts 3).
// ============================================================================

func TestInMemorySchedulerCarriesOptionsToJob(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)

	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if err := sched.Every(time.Minute).
		Job("digest", nil).
		Lane("bulk").
		Priority(7).
		MaxAttempts(5).
		RegisterAt(base); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}

	sched.dispatchDue(context.Background(), base.Add(time.Minute))
	jobs := rq.capturedJobs()
	if len(jobs) != 1 {
		t.Fatalf("fired %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Lane != "bulk" {
		t.Errorf("Lane = %q, want bulk", j.Lane)
	}
	if j.Priority != 7 {
		t.Errorf("Priority = %d, want 7", j.Priority)
	}
	if j.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", j.MaxAttempts)
	}
}

func TestInMemorySchedulerOmittedOptionsDefault(t *testing.T) {
	// dispatchDue applies the MaxAttempts=3 default itself so the Job
	// handed to Enqueue is always non-zero — matching the pre-options
	// behaviour even for custom Queue implementations.
	rq := &recordQueue{}
	sched := NewScheduler(rq)

	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if err := sched.Every(time.Minute).Job("digest", nil).RegisterAt(base); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}

	sched.dispatchDue(context.Background(), base.Add(time.Minute))
	jobs := rq.capturedJobs()
	if len(jobs) != 1 {
		t.Fatalf("fired %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Lane != "" {
		t.Errorf("Lane = %q, want empty", j.Lane)
	}
	if j.Priority != 0 {
		t.Errorf("Priority = %d, want 0", j.Priority)
	}
	if j.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3 (default)", j.MaxAttempts)
	}
}

// ============================================================================

func TestDurableSchedulerCarriesOptionsToJob(t *testing.T) {
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
		OwnerID: "opts", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	if err := scheduler.Every("digest", time.Minute).
		Job("send-digest", nil).
		Lane("bulk").
		Priority(9).
		MaxAttempts(2).
		RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := scheduler.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("run: %v", err)
	}
	jobs := pendingJobs(t, q)
	if len(jobs) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Lane != "bulk" {
		t.Errorf("Lane = %q, want bulk", j.Lane)
	}
	if j.Priority != 9 {
		t.Errorf("Priority = %d, want 9", j.Priority)
	}
	if j.MaxAttempts != 2 {
		t.Errorf("MaxAttempts = %d, want 2", j.MaxAttempts)
	}

	// The configured values persist in the schedules row.
	gotLane, gotPri, gotMax := readScheduleOptions(t, db, q, "digest")
	if gotLane != "bulk" || gotPri != 9 || gotMax != 2 {
		t.Errorf("persisted options = lane=%q pri=%d max=%d, want bulk/9/2",
			gotLane, gotPri, gotMax)
	}
}

func TestDurableSchedulerOmittedOptionsDefault(t *testing.T) {
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
		OwnerID: "default-opts", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	if err := scheduler.Every("digest", time.Minute).
		Job("send-digest", nil).
		RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := scheduler.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("run: %v", err)
	}
	jobs := pendingJobs(t, q)
	if len(jobs) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Lane != "" {
		t.Errorf("Lane = %q, want empty", j.Lane)
	}
	if j.Priority != 0 {
		t.Errorf("Priority = %d, want 0", j.Priority)
	}
	if j.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3 (default)", j.MaxAttempts)
	}
}

// Re-registering the same schedule ID updates the lane/priority/max-attempts
// WITHOUT resetting the persisted next_run watermark. Existing behaviour
// (watermark preserved across re-registration) is unchanged.
func TestDurableSchedulerReregisterUpdatesOptionsKeepsWatermark(t *testing.T) {
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
		OwnerID: "rereg", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	if err := scheduler.Every("digest", time.Minute).
		Job("send-digest", nil).
		Lane("bulk").
		Priority(1).
		MaxAttempts(2).
		RegisterAt(base); err != nil {
		t.Fatalf("register initial: %v", err)
	}

	// Re-register with new options at a LATER base. The watermark must not
	// reset to laterBase+interval; it must remain at the originally
	// persisted next_run (base + interval).
	laterBase := base.Add(2 * time.Hour)
	if err := scheduler.Every("digest", time.Minute).
		Job("send-digest", nil).
		Lane("urgent").
		Priority(8).
		MaxAttempts(6).
		RegisterAt(laterBase); err != nil {
		t.Fatalf("re-register: %v", err)
	}

	// Persisted options updated.
	lane, pri, max := readScheduleOptions(t, db, q, "digest")
	if lane != "urgent" || pri != 8 || max != 6 {
		t.Fatalf("re-registered options = lane=%q pri=%d max=%d, want urgent/8/6",
			lane, pri, max)
	}
	// Persisted next_run preserved (not reset to laterBase+interval).
	var nextRaw any
	if err := db.QueryRow("SELECT next_run FROM "+q.schedulerSchedulesTable()+
		" WHERE id=$1", "digest").Scan(&nextRaw); err != nil {
		t.Fatalf("read next_run: %v", err)
	}
	nextRun, err := queueTime(nextRaw)
	if err != nil {
		t.Fatalf("decode next_run: %v", err)
	}
	wantNext := base.Add(time.Minute).UTC()
	if !nextRun.UTC().Equal(wantNext) {
		t.Fatalf("next_run = %s, want %s (watermark must not reset on re-register)",
			nextRun, wantNext)
	}

	// And the fired job carries the NEW options.
	if err := scheduler.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("run: %v", err)
	}
	jobs := pendingJobs(t, q)
	if len(jobs) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Lane != "urgent" || j.Priority != 8 || j.MaxAttempts != 6 {
		t.Errorf("fired job options = lane=%q pri=%d max=%d, want urgent/8/6",
			j.Lane, j.Priority, j.MaxAttempts)
	}
}

// ============================================================================
// Migration: a schedules table created with the OLD schema (no lane / priority
// / max_attempts columns) is upgraded idempotently when NewDurableScheduler
// runs. The columns must be usable afterwards.
// ============================================================================

func TestDurableSchedulerMigratesScheduleOptionsColumns(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	// Create the schedules table by hand with the schema that shipped BEFORE
	// per-schedule options — no lane / priority / max_attempts columns. The
	// version column IS included so ensureScheduleVersionColumn does not run
	// first and mask the options-migration path.
	oldSchema := `CREATE TABLE queue_scheduler_schedules (
		id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		interval_ns BIGINT NOT NULL DEFAULT 0,
		cron_spec TEXT NOT NULL DEFAULT '',
		next_run DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		version BIGINT NOT NULL DEFAULT 0
	)`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("create old schema: %v", err)
	}

	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatalf("upgrade scheduler schema: %v", err)
	}

	cols := scheduleTableColumns(t, db, q)
	for _, want := range []string{"lane", "priority", "max_attempts"} {
		if _, ok := cols[want]; !ok {
			t.Errorf("column %q missing after migration: have %v", want, cols)
		}
	}

	// Idempotent: re-running migration must not error.
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatalf("re-run migration errored (not idempotent): %v", err)
	}
}

// ============================================================================
// Lane routing: a scheduled job whose Lane is reserved is claimed by the
// reserved-lane worker; a worker dedicated to a DIFFERENT lane cannot claim
// it. This proves the per-schedule Lane flows end-to-end through DBQueue's
// existing lane-isolation machinery — no second retry/backoff impl needed.
// ============================================================================

func TestScheduledBulkLaneJobClaimedByBulkWorker(t *testing.T) {
	// openDurableSchedulerDB sets MaxOpenConns(8) — required for the durable
	// scheduler's transactions to coexist with the worker pool's dequeues
	// (the newSQLiteDB helper pins a single connection and the worker's
	// polling dequeue can stall behind the scheduler's tx).
	db := openDurableSchedulerDB(t)
	q, err := NewDBQueue(db, WithWorkers(0), WithDBLaneWorkers("bulk", 1))
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var ran atomic.Int32
	q.RegisterHandler("reindex", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})
	q.Start(ctx)

	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "bulk-lane", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	// Anchor the schedule an hour in the past so the fired tick's
	// scheduled_at lands BEHIND wall-clock — the bulk worker's polling
	// dequeue compares scheduled_at against time.Now(), so a future tick
	// would never become eligible within the test's window.
	now := time.Now().UTC()
	base := now.Add(-time.Hour)
	if err := sched.Every("bulk-reindex", time.Minute).
		Job("reindex", nil).
		Lane("bulk").
		RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := sched.RunOnce(ctx, now); err != nil {
		t.Fatalf("fire: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ran.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := ran.Load(); got != 1 {
		pending := pendingJobs(t, q)
		t.Fatalf("bulk-lane worker did not claim the scheduled bulk job: ran=%d, pending=%d (jobs=%v)",
			got, len(pending), pending)
	}
	q.Close()
}

func TestScheduledBulkLaneJobNotClaimableByOtherLaneWorker(t *testing.T) {
	db := openDurableSchedulerDB(t)
	// Only an "urgent" lane worker: NO shared workers, NO bulk worker. A
	// bulk-lane scheduled job must stay pending — the urgent worker cannot
	// claim another lane's job.
	q, err := NewDBQueue(db, WithWorkers(0), WithDBLaneWorkers("urgent", 1))
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	// 3 s poll window: a buggy (non-lane-isolated) dequeue would claim the
	// job near-instantly, so this is plenty of time to surface a routing
	// regression while keeping the test fast.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var ran atomic.Int32
	q.RegisterHandler("reindex", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})
	q.Start(ctx)

	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "no-bulk", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	// Anchor the schedule an hour in the past so the fired tick's
	// scheduled_at lands BEHIND wall-clock. The urgent worker's polling
	// dequeue gates on scheduled_at <= now, so a future tick would never
	// become eligible for ANY worker and the test would pass vacuously —
	// masking a broken lane router. Mirror the positive test's anchoring.
	now := time.Now().UTC()
	base := now.Add(-time.Hour)
	if err := sched.Every("bulk-reindex", time.Minute).
		Job("reindex", nil).
		Lane("bulk").
		RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := sched.RunOnce(ctx, now); err != nil {
		t.Fatalf("fire: %v", err)
	}

	// Give the urgent worker a real chance to claim it (if lane isolation
	// were broken it would happen near-instantly), then assert it never did.
	<-ctx.Done()
	if got := ran.Load(); got != 0 {
		t.Fatalf("urgent-lane worker claimed a bulk-lane scheduled job: ran %d, want 0", got)
	}
	q.Close()

	// The job must still be pending — proving lane isolation routed it away
	// from the only available worker.
	stats, err := q.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["pending"] < 1 {
		t.Fatalf("bulk-lane scheduled job vanished: pending=%d, want >=1", stats["pending"])
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func readScheduleOptions(t *testing.T, db *sql.DB, q *DBQueue, id string) (lane string, priority, maxAttempts int) {
	t.Helper()
	row := db.QueryRow("SELECT lane, priority, max_attempts FROM "+
		q.schedulerSchedulesTable()+" WHERE id=$1", id)
	if err := row.Scan(&lane, &priority, &maxAttempts); err != nil {
		t.Fatalf("read schedule options for %q: %v", id, err)
	}
	return
}

func scheduleTableColumns(t *testing.T, db *sql.DB, q *DBQueue) map[string]struct{} {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + q.schedulerSchedulesTable() + ")")
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan pragma: %v", err)
		}
		out[name] = struct{}{}
	}
	return out
}
