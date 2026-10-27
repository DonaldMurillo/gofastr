package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// ============================================================================
// A tick fires at most once across replicas. runTick releases the lease as
// soon as the tick's jobs finish, so replica B's timer for the same minute,
// firing a moment after A released, found the lease free and ran the jobs
// a second time: nothing recorded which minute was consumed. A lease that
// implements TickLeaderElection now claims the minute itself.
// ============================================================================

// tickBook is the shared "last claimed minute" record two fake leases
// coordinate on, standing in for the Postgres ticks table.
type tickBook struct {
	mu   sync.Mutex
	last time.Time
}

// fakeTickLease is a TickLeaderElection whose lock is always free (the
// previous holder already released it), so only the tick claim can refuse.
type fakeTickLease struct{ book *tickBook }

func (l fakeTickLease) Acquire(context.Context) (bool, func(), error) {
	return true, func() {}, nil
}

func (l fakeTickLease) AcquireTick(_ context.Context, tick time.Time) (bool, func(), error) {
	l.book.mu.Lock()
	defer l.book.mu.Unlock()
	if !tick.After(l.book.last) {
		return false, nil, nil
	}
	l.book.last = tick
	return true, func() {}, nil
}

// countingScheduler returns a scheduler with one every-minute job that
// counts its runs into fired.
func countingScheduler(t *testing.T, le LeaderElection, fired *atomic.Int32) *Scheduler {
	t.Helper()
	s := NewScheduler()
	s.WithLeaderElection(le)
	if err := s.Register(CronJob{
		Name: "j", Spec: "* * * * *",
		Run: func(context.Context) error { fired.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestTickClaimFiresOncePerMinute: two schedulers sharing a tick record
// run the same minute one after the other; the job runs once, and the
// next minute still fires.
func TestTickClaimFiresOncePerMinute(t *testing.T) {
	book := &tickBook{}
	var fired atomic.Int32
	a := countingScheduler(t, fakeTickLease{book}, &fired)
	b := countingScheduler(t, fakeTickLease{book}, &fired)
	minute := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if !a.runTick(ctx, minute.Add(3*time.Millisecond)) {
		t.Fatal("replica A did not fire the first tick")
	}
	a.inflight.Wait()
	if b.runTick(ctx, minute.Add(40*time.Millisecond)) {
		t.Error("replica B fired a minute replica A already claimed")
	}
	b.inflight.Wait()
	if got := fired.Load(); got != 1 {
		t.Fatalf("job ran %d times for one minute across two replicas, want 1", got)
	}
	if !b.runTick(ctx, minute.Add(time.Minute)) {
		t.Fatal("replica B did not fire the next minute")
	}
	b.inflight.Wait()
	if got := fired.Load(); got != 2 {
		t.Fatalf("job ran %d times over two minutes, want 2", got)
	}
}

// TestPGTickClaimFiresOnce: the same sequence through two
// PostgresAdvisoryLease values on one key, waiting for A's lock release
// before B's tick.
func TestPGTickClaimFiresOnce(t *testing.T) {
	db := pgtest.DB(t)
	db.SetMaxOpenConns(3) // A's or B's pinned lock conn, plus the release probe
	const key = 7340021
	var fired atomic.Int32
	a := countingScheduler(t, NewPostgresAdvisoryLease(db, key), &fired)
	b := countingScheduler(t, NewPostgresAdvisoryLease(db, key), &fired)
	probe := NewPostgresAdvisoryLease(db, key)
	minute := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	waitReleased := func(s *Scheduler) {
		t.Helper()
		s.inflight.Wait()
		deadline := time.Now().Add(5 * time.Second)
		for {
			held, rel, err := probe.Acquire(ctx)
			if err != nil {
				t.Fatalf("probe acquire: %v", err)
			}
			if held {
				rel()
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("lease never released after the tick's jobs finished")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	if !a.runTick(ctx, minute.Add(3*time.Millisecond)) {
		t.Fatal("replica A did not fire the first tick")
	}
	waitReleased(a)
	if b.runTick(ctx, minute.Add(40*time.Millisecond)) {
		t.Error("replica B fired a minute replica A already claimed")
	}
	waitReleased(b)
	if got := fired.Load(); got != 1 {
		t.Fatalf("job ran %d times for one minute across two replicas, want 1", got)
	}
	if !b.runTick(ctx, minute.Add(time.Minute)) {
		t.Fatal("replica B did not fire the next minute")
	}
	waitReleased(b)
	if got := fired.Load(); got != 2 {
		t.Fatalf("job ran %d times over two minutes, want 2", got)
	}
	// A minute older than the last claim (a replica whose clock runs
	// behind) is refused too: claims are monotonic.
	if a.runTick(ctx, minute) {
		t.Error("replica A fired a minute older than the last claimed one")
	}
	waitReleased(a)
}
