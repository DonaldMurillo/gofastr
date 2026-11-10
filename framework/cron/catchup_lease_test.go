package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// A late tick never contends with a lease it took itself. The catch-up walk
// used to acquire the leader lease once per walked minute, back to back,
// while each minute's lease was handed back on a goroutine: the next
// Acquire met the walk's own previous lease, a real try-lock refused it,
// and the minute was dropped silently. The current minute is the last one
// walked and the only one where wildcard jobs are judged, so a late tick on
// a single replica ran nothing. The doubles here model the lock; the older
// ones in this package (fakeTickLease, releaseLog) always grant it.
//
// The contract: a plain lease is taken once for the whole walk and held
// until the jobs the walk started finish; a tick lease is taken per minute,
// after the hand-back of the minute before it has returned.
// ============================================================================

// mutexLease is a plain LeaderElection with real mutual exclusion, the
// contract the interface documents ("mutual exclusion while the lease is
// held"): Acquire is a try-lock, release unlocks.
type mutexLease struct {
	held     atomic.Bool
	refusals atomic.Int32
}

func (l *mutexLease) Acquire(context.Context) (bool, func(), error) {
	if !l.held.CompareAndSwap(false, true) {
		l.refusals.Add(1)
		return false, nil, nil
	}
	return true, func() { l.held.Store(false) }, nil
}

// sessionTickLease models PostgresAdvisoryLease: a session-scoped
// try-lock plus a monotonic "last claimed minute" record. The lock is
// free again only once release has run. unlock, when set, is called by
// release before it frees the lock.
type sessionTickLease struct {
	mu       sync.Mutex
	locked   bool
	last     time.Time
	claimed  []time.Time
	refusals int
	unlock   func()
}

func (l *sessionTickLease) Acquire(ctx context.Context) (bool, func(), error) {
	return l.AcquireTick(ctx, time.Time{})
}

func (l *sessionTickLease) AcquireTick(_ context.Context, tick time.Time) (bool, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locked { // pg_try_advisory_lock returned false
		l.refusals++
		return false, nil, nil
	}
	if !tick.After(l.last) { // the conditional upsert matched no row
		return false, nil, nil
	}
	l.locked = true
	l.last = tick
	l.claimed = append(l.claimed, tick)
	return true, func() {
		if l.unlock != nil {
			l.unlock()
		}
		l.mu.Lock()
		l.locked = false
		l.mu.Unlock()
	}, nil
}

func (l *sessionTickLease) claims() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.claimed)
}

func registerCounter(t *testing.T, s *Scheduler, name, spec string, n *atomic.Int32) {
	t.Helper()
	if err := s.Register(CronJob{Name: name, Spec: spec, Run: func(context.Context) error {
		n.Add(1)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
}

// A single replica wakes two minutes late. A fixed-time job fell in the
// gap and is still running when the walk reaches the current minute: the
// wildcard job runs once there all the same, and the lease stays held
// against a peer until the gap's job ends.
func TestCatchUpPlainLeaseRunsCurrentMinute(t *testing.T) {
	s := NewScheduler()
	lease := &mutexLease{}
	s.WithLeaderElection(lease)

	releaseFixed := make(chan struct{})
	var fixedRuns, wildcardRuns atomic.Int32
	if err := s.Register(CronJob{Name: "fixed", Spec: "1 12 * * *", Run: func(context.Context) error {
		fixedRuns.Add(1)
		<-releaseFixed // a job that takes a little while, e.g. a report
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	registerCounter(t, s, "wild", "* * * * *", &wildcardRuns)

	last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	tick := last.Add(time.Minute)    // the 12:01 tick ...
	now := tick.Add(2 * time.Minute) // ... arrives at 12:03
	s.catchUp(context.Background(), last, tick, now)

	deadline := time.Now().Add(2 * time.Second)
	for wildcardRuns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := lease.refusals.Load(); got != 0 {
		t.Errorf("the walk was refused its own lease %d times; it takes a plain lease once", got)
	}
	// The gap's job is still running, so the lease is what keeps a peer
	// replica from firing these minutes too.
	if held, _, _ := lease.Acquire(context.Background()); held {
		t.Fatal("plain lease released while a job the walk started still runs")
	}
	close(releaseFixed)
	s.inflight.Wait()

	if got := fixedRuns.Load(); got != 1 {
		t.Fatalf("fixed-time job in the gap ran %d times, want 1", got)
	}
	if got := wildcardRuns.Load(); got != 1 {
		t.Fatalf("wildcard job ran %d times at the current minute, want 1", got)
	}
	deadline = time.Now().Add(2 * time.Second)
	for lease.held.Load() {
		if time.Now().After(deadline) {
			t.Fatal("plain lease never released after the walk's jobs finished")
		}
		time.Sleep(time.Millisecond)
	}
}

// The same walk with no job in the gap: nothing runs at the walked minute,
// and the current minute still fires.
func TestCatchUpPlainLeaseIdleGapRunsCurrentMinute(t *testing.T) {
	const rounds = 50
	lost := 0
	for range rounds {
		s := NewScheduler()
		lease := &mutexLease{}
		s.WithLeaderElection(lease)
		var wildcardRuns atomic.Int32
		registerCounter(t, s, "wild", "* * * * *", &wildcardRuns)
		last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
		tick := last.Add(time.Minute)
		now := tick.Add(time.Minute) // one minute late: walk 12:01, then 12:02
		s.catchUp(context.Background(), last, tick, now)
		s.inflight.Wait()
		if wildcardRuns.Load() != 1 {
			lost++
		}
	}
	if lost > 0 {
		t.Fatalf("a tick one minute late ran no job in %d of %d rounds", lost, rounds)
	}
}

// "Under leader election the walk claims each minute in order, the
// current one included" (cron.md). With one replica and nobody else
// holding the lock, every walked minute is claimed.
func TestCatchUpTickLeaseClaimsEveryMinute(t *testing.T) {
	const rounds = 50
	skippedRounds, lostCurrent := 0, 0
	for range rounds {
		s := NewScheduler()
		lease := &sessionTickLease{}
		s.WithLeaderElection(lease)
		var wildcardRuns atomic.Int32
		registerCounter(t, s, "wild", "* * * * *", &wildcardRuns)
		last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
		tick := last.Add(time.Minute)
		now := tick.Add(4 * time.Minute) // five minutes to walk: 12:01..12:05
		s.catchUp(context.Background(), last, tick, now)
		s.inflight.Wait()
		if lease.claims() != 5 {
			skippedRounds++
		}
		if wildcardRuns.Load() != 1 {
			lostCurrent++
		}
	}
	if skippedRounds > 0 || lostCurrent > 0 {
		t.Fatalf("in %d of %d late ticks the walk skipped a minute behind its own previous claim; "+
			"in %d of them the every-minute job did not run", skippedRounds, rounds, lostCurrent)
	}
}

// Two ticks back to back (a walk that ends just before the next minute
// boundary) do not contend either: the second waits for the first one's
// hand-back, whichever kind of lease it is.
func TestBackToBackTicksWaitForHandBack(t *testing.T) {
	minute := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	for name, lease := range map[string]LeaderElection{
		"plain lease, no job running": &mutexLease{},
		"tick lease":                  &sessionTickLease{},
	} {
		for round := range 50 {
			s := NewScheduler()
			s.WithLeaderElection(lease)
			at := minute.Add(time.Duration(2*round) * time.Minute)
			if !s.runTick(context.Background(), at) {
				t.Fatalf("%s: first tick did not fire", name)
			}
			if !s.runTick(context.Background(), at.Add(time.Minute)) {
				t.Fatalf("%s: the next tick was refused the lease the tick before it had not yet handed back", name)
			}
			// The next round's scheduler is a different replica: it gets
			// the lease once this one's last hand-back has returned.
			if !s.awaitHandBack(context.Background()) {
				t.Fatalf("%s: hand-back never returned", name)
			}
		}
	}
}

// Waiting for a hand-back never pins the loop: Stop and a cancelled context
// both end a walk that is waiting on a release that has not returned.
func TestCatchUpWaitingOnHandBackHonoursStop(t *testing.T) {
	last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	for name, end := range map[string]func(s *Scheduler, cancel context.CancelFunc){
		"stop":   func(s *Scheduler, _ context.CancelFunc) { close(s.stop) },
		"cancel": func(_ *Scheduler, cancel context.CancelFunc) { cancel() },
	} {
		s := NewScheduler()
		unblock := make(chan struct{})
		lease := &sessionTickLease{unlock: func() { <-unblock }}
		s.WithLeaderElection(lease)
		ctx, cancel := context.WithCancel(context.Background())
		walked := make(chan struct{})
		go func() {
			defer close(walked)
			s.catchUp(ctx, last, last.Add(time.Minute), last.Add(3*time.Minute))
		}()
		// 12:01 is claimed and its release hangs; the walk now waits.
		deadline := time.Now().Add(2 * time.Second)
		for lease.claims() == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		select {
		case <-walked:
			t.Fatalf("%s: the walk went on past a lease it had not handed back (claimed %d minutes)", name, lease.claims())
		case <-time.After(20 * time.Millisecond):
		}
		end(s, cancel)
		select {
		case <-walked:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: the walk is pinned on a hung release", name)
		}
		if got := lease.claims(); got != 1 {
			t.Errorf("%s: claimed %d minutes, want 1: nothing is claimed after stop", name, got)
		}
		close(unblock)
		cancel()
	}
}
