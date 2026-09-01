package queue

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// ============================================================================
// Property: concurrent Postgres claims hand every job to exactly one worker
// with zero lock-contention errors — the serialized-claim contract the
// SQLite twins in this pass pin on the SQLite dialect
// (TestSQLiteDequeueConcurrentNoBusy here, TestClaimDeliveriesConcurrentNoBusy
// in framework/outbox), where SELECT-then-UPDATE leaked SQLITE_BUSY/
// SQLITE_LOCKED to callers. Surfaces: DBQueue.dequeuePostgres (UPDATE ...
// FOR UPDATE SKIP LOCKED) and, for the fenced-lease property,
// DurableScheduler.acquireLease / commitOccurrences under replica clock
// skew.
//
// Pass 1 traced the Postgres paths as canonical (single atomic statement,
// SKIP LOCKED) and deferred these twins to a reachable Postgres (pgtest).
// They are expected GREEN-PIN; a RED here is a NEW finding against that
// canonical status.
// ============================================================================

// pgLockClass reports whether an error message is Postgres lock contention
// (55P03 lock not available, 40P01 deadlock detected, 40001 serialization
// failure) rather than a real fault. The SKIP LOCKED claim should make the
// first unreachable for queue claims; none of the three can legitimately
// surface from the shapes asserted here.
func pgLockClass(msg string) bool {
	s := strings.ToLower(msg)
	return strings.Contains(s, "lock") ||
		strings.Contains(s, "could not serialize access")
}

// TestPGDequeueConcurrentNoBusy is the Postgres twin of
// TestSQLiteDequeueConcurrentNoBusy: 8 concurrent claimants released by a
// barrier drain 200 jobs through dequeuePostgres's FOR UPDATE SKIP LOCKED
// claim. On Postgres the claim is one atomic statement, so there is no
// legitimate contention error of any class; exactly-once claim attribution
// and full drain are asserted alongside.
func TestPGDequeueConcurrentNoBusy(t *testing.T) {
	db := pgtest.DB(t)
	const workers = 8
	// pgtest pins MaxOpenConns(1) for advisory-lock suites; the queue claim
	// uses row locks only. SKIP LOCKED is untestable through a single
	// serialized connection — widen the pool so the claimants genuinely
	// race. search_path rides the DSN, so every pooled connection stays
	// schema-scoped.
	db.SetMaxOpenConns(workers)

	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	if q.dialect != dialectPostgres {
		t.Fatalf("dialect = %v, want postgres (pgtest returned a non-postgres DB)", q.dialect)
	}
	q.RegisterHandler("work", func(_ context.Context, _ Job) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const total = 200
	for i := 0; i < total; i++ {
		if err := q.Enqueue(ctx, Job{ID: fmtID(i), Type: "work"}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	types := q.eligibleTypes()

	start := make(chan struct{})
	var wg sync.WaitGroup
	claims := make([][]string, workers)
	errs := make([][]string, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			empty := 0
			for empty < 3 {
				if ctx.Err() != nil {
					errs[w] = append(errs[w], ctx.Err().Error())
					return
				}
				job, err := q.dequeue(ctx, "", types)
				switch {
				case err == nil:
					empty = 0
					claims[w] = append(claims[w], job.ID)
				case errors.Is(err, ErrNoJob):
					empty++
				default:
					errs[w] = append(errs[w], err.Error())
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(w)
	}
	close(start)
	wg.Wait()

	var busy, other, all []string
	for w := 0; w < workers; w++ {
		all = append(all, claims[w]...)
		for _, e := range errs[w] {
			if pgLockClass(e) {
				busy = append(busy, e)
			} else {
				other = append(other, e)
			}
		}
	}
	if len(busy) > 0 {
		t.Errorf("serialized-claim contract violated: %d lock-contention errors from concurrent dequeue; first: %q",
			len(busy), busy[0])
	}
	if len(other) > 0 {
		t.Errorf("concurrent dequeue surfaced unexpected errors; first: %q", other[0])
	}
	seen := make(map[string]int, total)
	for _, id := range all {
		seen[id]++
	}
	var dupes []string
	for id, n := range seen {
		if n > 1 {
			dupes = append(dupes, id+" x"+strconv.Itoa(n))
		}
	}
	if len(dupes) > 0 {
		t.Errorf("same job claimed by multiple workers (at-most-once breach): %v", dupes)
	}
	if len(all) != total {
		t.Errorf("claimed %d of %d jobs", len(all), total)
	}
	var pending int
	if err := db.QueryRow("SELECT COUNT(*) FROM queue_jobs WHERE status='pending'").Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 0 {
		t.Errorf("%d jobs never claimed", pending)
	}
}

// TestPGLeaseSkewStillFencesOldOwner is the Postgres twin of
// TestLeaseSkewStillFencesOldOwner (durable_scheduler_security_test.go):
// replica B's clock runs >= leaseDuration ahead of A's, B legitimately
// steals the lease while A's commit is paused mid-flight, and A resumes on
// its lagging clock — where the new lease's expires_at is still in A's
// future, so only the owner+fence re-check inside commitOccurrences can
// fence the stale claim. Postgres stores TIMESTAMPTZ at microsecond
// precision; every timestamp here is whole seconds, so no precision
// caveats apply.
func TestPGLeaseSkewStillFencesOldOwner(t *testing.T) {
	db := pgtest.DB(t)
	q1 := newDurableTestQueue(t, db)
	q2 := newDurableTestQueue(t, db)
	base := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	lease := 30 * time.Second

	sA, err := NewDurableScheduler(q1, DurableSchedulerConfig{
		OwnerID: "pg-skew-lagged", LeaseDuration: lease,
	})
	if err != nil {
		t.Fatal(err)
	}
	sB, err := NewDurableScheduler(q2, DurableSchedulerConfig{
		OwnerID: "pg-skew-ahead", LeaseDuration: lease,
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
	if owner != "pg-skew-ahead" || fence != 2 {
		t.Fatalf("lease after skewed handoff: owner=%q fence=%d, want pg-skew-ahead/2", owner, fence)
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
