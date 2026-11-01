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
// The lease never waits on a job from another tick. runTick released the
// lease after s.inflight.Wait(), the scheduler-wide WaitGroup, so one long
// or hung job on the leader held the lease through every later tick: with
// a Postgres lease a 20-minute @hourly report skipped 20 minutes of every
// other job on every replica. A tick-claiming lease is released as soon as
// the claim is made (the claim alone keeps the minute at most once); a plain
// lease is held through its own tick's jobs only.
// ============================================================================

// releaseLog is a LeaderElection double that hands out a fresh release per
// Acquire and records which acquisitions were released.
type releaseLog struct {
	mu       sync.Mutex
	n        int
	released map[int]chan struct{}
}

func newReleaseLog() *releaseLog { return &releaseLog{released: map[int]chan struct{}{}} }

func (l *releaseLog) Acquire(context.Context) (bool, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
	ch := make(chan struct{})
	l.released[l.n] = ch
	var once sync.Once
	return true, func() { once.Do(func() { close(ch) }) }, nil
}

func (l *releaseLog) done(i int) <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.released[i]
}

// tickReleaseLog is releaseLog with a tick claim.
type tickReleaseLog struct{ *releaseLog }

func (l tickReleaseLog) AcquireTick(ctx context.Context, _ time.Time) (bool, func(), error) {
	return l.Acquire(ctx)
}

// blockingScheduler registers one every-minute job whose first firing
// closes started and then blocks on gate; later firings return at once.
func blockingScheduler(t *testing.T, le LeaderElection) (*Scheduler, chan struct{}, <-chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	s := NewScheduler()
	s.WithLeaderElection(le)
	if err := s.Register(CronJob{Name: "j", Spec: "* * * * *", Run: func(context.Context) error {
		if calls.Add(1) == 1 {
			close(started)
			<-gate
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
		s.inflight.Wait()
	})
	return s, gate, started
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(what)
	}
}

func TestTickLeaseFreedBeforeJobEnds(t *testing.T) {
	lease := tickReleaseLog{newReleaseLog()}
	s, _, _ := blockingScheduler(t, lease)
	if !s.runTick(context.Background(), time.Now()) {
		t.Fatal("leader did not fire")
	}
	waitClosed(t, lease.done(1), "tick lease still held while the tick's job runs; a long job blocks every later tick cluster-wide")
}

func TestPlainLeaseSkipsOtherTicksJobs(t *testing.T) {
	lease := newReleaseLog()
	s, _, started := blockingScheduler(t, lease)
	minute := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if !s.runTick(context.Background(), minute) {
		t.Fatal("first tick did not fire")
	}
	// The first tick's job must be the one that blocks: without this wait
	// the second tick's goroutine can run first and take the blocking call.
	waitClosed(t, started, "first tick's job never started")
	if !s.runTick(context.Background(), minute.Add(time.Minute)) {
		t.Fatal("second tick did not fire")
	}
	waitClosed(t, lease.done(2), "second tick's lease waits on the first tick's still-running job")
	select {
	case <-lease.done(1):
		t.Fatal("plain lease released while its own tick's job still runs")
	default:
	}
}

// TestPGLongJobKeepsNextTick: with the Postgres lease, a job still running
// from one minute does not stop the next minute from firing.
func TestPGLongJobKeepsNextTick(t *testing.T) {
	db := pgtest.DB(t)
	db.SetMaxOpenConns(3)
	var fired atomic.Int32
	gate := make(chan struct{})
	s := NewScheduler()
	s.WithLeaderElection(NewPostgresAdvisoryLease(db, 7340023))
	if err := s.Register(CronJob{Name: "j", Spec: "* * * * *", Run: func(context.Context) error {
		if fired.Add(1) == 1 {
			<-gate
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	defer func() { close(gate); s.inflight.Wait() }()
	minute := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if !s.runTick(ctx, minute) {
		t.Fatal("first minute did not fire")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !s.runTick(ctx, minute.Add(time.Minute)) {
		if time.Now().After(deadline) {
			t.Fatal("next minute skipped while the first minute's job runs: the lease is held through it")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
