package queue

import (
	"context"
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
