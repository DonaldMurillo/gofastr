package queue

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// Property: a fenced lease keeps occurrence commits exactly-once even when
// replica clocks disagree by at least the lease duration. Under that skew
// the takeover is legitimate AND the stale claimant's expiry clause passes
// (the new lease's expires_at is still in the lagging clock's future), so
// the owner+fence re-check inside commitOccurrences must carry the fencing
// alone. Surface: DurableScheduler.acquireLease / commitOccurrences.
// ============================================================================

// TestLeaseSkewStillFencesOldOwner is the clock-skew attack shape on the
// pinned lease-handoff property (durable_scheduler_test.go pins the same
// fencing with aligned clocks): replica B's clock runs >= leaseDuration
// ahead of A's, B steals the lease while A's commit is paused mid-flight,
// and A resumes with its lagging clock.
func TestLeaseSkewStillFencesOldOwner(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q1 := newDurableTestQueue(t, db)
	q2 := newDurableTestQueue(t, db)
	base := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	lease := 30 * time.Second

	sA, err := NewDurableScheduler(q1, DurableSchedulerConfig{
		OwnerID: "skew-lagged", LeaseDuration: lease,
	})
	if err != nil {
		t.Fatal(err)
	}
	sB, err := NewDurableScheduler(q2, DurableSchedulerConfig{
		OwnerID: "skew-ahead", LeaseDuration: lease,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sA.Every("digest", time.Minute).Job("send-digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}

	paused := make(chan struct{})
	resume := make(chan struct{})
	sA.beforeOccurrenceCommit = func() {
		close(paused)
		<-resume
	}
	firstDone := make(chan error, 1)
	go func() {
		// A's clock: base+60. Acquires the lease (expires base+90, fence 1)
		// then partitions between evaluation and its ownership re-check.
		firstDone <- sA.RunOnce(context.Background(), base.Add(time.Minute))
	}()
	<-paused

	// B's clock runs 65s ahead of A's (>= leaseDuration): from B's view the
	// lease has expired, the takeover legitimately fires, the fence bumps,
	// and B commits the occurrence while A is partitioned.
	bNow := base.Add(time.Minute + 65*time.Second)
	if err := sB.RunOnce(context.Background(), bNow); err != nil {
		t.Fatalf("skewed-ahead RunOnce: %v", err)
	}

	// Resume A. Its ownership re-check runs with A's lagging clock: the new
	// lease's expires_at (bNow+lease) is still far in A's future, so the
	// expiry clause passes for A — only the owner+fence mismatch can fence.
	close(resume)
	if err := <-firstDone; err != nil {
		t.Fatalf("lagged claimant RunOnce: %v", err)
	}

	if jobs := pendingJobs(t, q1); len(jobs) != 1 {
		t.Fatalf("skewed handoff enqueued %d jobs, want exactly 1", len(jobs))
	}
	if got := occurrenceStatuses(t, q1)["enqueued"]; got != 1 {
		t.Fatalf("skewed handoff enqueued occurrences = %d, want 1", got)
	}
	var owner string
	var fence int64
	if err := db.QueryRow(
		"SELECT owner_id, fence FROM "+q1.schedulerLeaseTable()+" WHERE name=$1",
		durableSchedulerLeaseName,
	).Scan(&owner, &fence); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if owner != "skew-ahead" || fence != 2 {
		t.Fatalf("lease after skewed handoff: owner=%q fence=%d, want skew-ahead/2", owner, fence)
	}

	// The lagging replica must not steal the lease back either: its takeover
	// predicate (expires_at <= A_now) is false even on A's lagging clock,
	// and its stale fence is dead regardless of what its clock says.
	if err := sA.RunOnce(context.Background(), base.Add(time.Minute+5*time.Second)); err != nil {
		t.Fatalf("lagged reclaim RunOnce: %v", err)
	}
	if jobs := pendingJobs(t, q1); len(jobs) != 1 {
		t.Fatalf("lagged reclaim enqueued %d jobs, want still exactly 1", len(jobs))
	}
	if got := occurrenceStatuses(t, q1)["enqueued"]; got != 1 {
		t.Fatalf("lagged reclaim occurrences = %d, want 1", got)
	}
}

// ============================================================================
// Pins one corrupted schedule row being fatal to the whole evaluation pass,
// found by the 2026-09-04 red-probe round; fixed in loadDue by decoding per
// row and skip-and-logging poison rows (exactly as runOnce already does for
// dueTicks errors), with Start surviving evaluation/wake errors instead of
// returning.
// Property: one corrupted schedule row must be skipped (observed, logged),
// never fatal to the evaluation pass — every OTHER schedule must still
// fire, including across Start's loop.
// Surfaces: durable_scheduler.go loadDue (wholesale decode failure aborted
// runOnce), Start (any runOnce/nextWakeDelay error returned, permanently
// ending the loop), runOnce's dueTicks error branch (the in-repo precedent
// this fix mirrors).
// ============================================================================

// TestCorruptRowDoesNotStopSchedules: a schedule row whose next_run will
// not decode must be skipped with the healthy due schedule still firing;
// RunOnce must not return the poison row's error for the whole pass.
func TestCorruptRowDoesNotStopSchedules(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{})
	if err != nil {
		t.Fatalf("new durable scheduler: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Two due schedules (anchored so next_run is already in the past).
	base := now.Add(-2 * time.Hour)
	if err := s.Every("healthy", time.Hour).Job("healthy.job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register healthy: %v", err)
	}
	if err := s.Every("poison", time.Hour).Job("poison.job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register poison: %v", err)
	}
	// Corrupt one row's next_run (the partial-write / bad-repair shape):
	// a value that still sorts as due in loadDue's WHERE but fails queueTime
	// decode in both the driver and the parser.
	if _, err := db.Exec(fmt.Sprintf(
		"UPDATE %s SET next_run = '0000-00-00 00:00:00' WHERE id = 'poison'", q.schedulerSchedulesTable())); err != nil {
		t.Fatalf("corrupt poison row: %v", err)
	}

	if err := s.RunOnce(ctx, now); err != nil {
		t.Fatalf("SECURITY: [queue] one corrupt schedule row is fatal to the whole evaluation pass "+
			"(runOnce aborts; Start's loop exits and every schedule on the replica stops firing until restart): %v", err)
	}

	var fired int
	if err := db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE type = 'healthy.job'", q.qt())).Scan(&fired); err != nil {
		t.Fatalf("count healthy jobs: %v", err)
	}
	if fired != 1 {
		t.Fatalf("SECURITY: [queue] healthy due schedule did not fire while a sibling row was corrupt: "+
			"enqueued %d jobs, want 1 — the poison row stalled evaluation of every schedule", fired)
	}
}

// TestStartSurvivesPoisonRowAndKeepsFiring: with a poison row permanently
// parked at the front of next_run ordering, Start's wake computation
// (nextWakeDelay) errors on every pass; the loop must log and keep firing
// on the heartbeat instead of returning and stopping every schedule.
func TestStartSurvivesPoisonRowAndKeepsFiring(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	// Short lease so the heartbeat (lease/3) is test-fast.
	s, err := NewDurableScheduler(q, DurableSchedulerConfig{LeaseDuration: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("new durable scheduler: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now().UTC().Truncate(time.Second)
	base := now.Add(-2 * time.Hour)
	if err := s.Every("healthy", time.Hour).Job("healthy.job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register healthy: %v", err)
	}
	if err := s.Every("poison", time.Hour).Job("poison.job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register poison: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf(
		"UPDATE %s SET next_run = '0000-00-00 00:00:00' WHERE id = 'poison'", q.schedulerSchedulesTable())); err != nil {
		t.Fatalf("corrupt poison row: %v", err)
	}

	startErr := make(chan error, 1)
	go func() { startErr <- s.Start(ctx) }()

	// The healthy schedule must fire even though the poison row makes every
	// nextWakeDelay call fail (its undecodable next_run sorts first).
	deadline := time.After(5 * time.Second)
	fired := false
	for !fired {
		select {
		case <-deadline:
			t.Fatal("SECURITY: [queue] healthy schedule never fired under Start while a poison row " +
				"made every wake computation fail: the loop exited (or never evaluated) instead of " +
				"surviving on the heartbeat")
		case err := <-startErr:
			t.Fatalf("SECURITY: [queue] Start returned %v on a poison row: every schedule on the "+
				"replica stops firing until process restart", err)
		default:
		}
		var n int
		if err := db.QueryRow(fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE type = 'healthy.job'", q.qt())).Scan(&n); err != nil {
			t.Fatalf("count healthy jobs: %v", err)
		}
		if n >= 1 {
			fired = true
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The loop must still be alive past several heartbeats of poison-row
	// wake failures (Start surviving, not just slow to die).
	select {
	case err := <-startErr:
		t.Fatalf("SECURITY: [queue] Start returned %v after the heartbeat windows", err)
	case <-time.After(time.Second):
	}
	cancel()
}
