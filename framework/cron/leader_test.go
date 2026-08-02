package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// stubLease is a deterministic LeaderElection double for testing the
// scheduler's leader-gated tick path without a database.
type stubLease struct {
	held     bool
	released chan struct{}
}

func newStubLease(held bool) *stubLease {
	return &stubLease{held: held, released: make(chan struct{})}
}

func (l *stubLease) Acquire(ctx context.Context) (bool, func(), error) {
	if !l.held {
		return false, nil, nil
	}
	return true, func() {
		select {
		case <-l.released:
		default:
			close(l.released)
		}
	}, nil
}

// TestRunTick_SkipsWhenNotLeader: when leader election is configured and this
// replica does NOT win the lease, the tick skips every job — so a second
// replica sharing the lease never double-fires the schedule.
func TestRunTick_SkipsWhenNotLeader(t *testing.T) {
	var fired atomic.Int32
	s := NewScheduler()
	s.WithLeaderElection(newStubLease(false))
	if err := s.Register(CronJob{
		Name: "j", Spec: "* * * * *",
		Run: func(context.Context) error { fired.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if s.runTick(context.Background(), time.Now()) {
		t.Error("non-leader runTick reported it fired")
	}
	if fired.Load() != 0 {
		t.Errorf("non-leader fired %d jobs, want 0", fired.Load())
	}
}

// TestRunTick_FiresWhenLeader: when this replica wins the lease, the tick fires
// matching jobs and then releases the lease.
func TestRunTick_FiresWhenLeader(t *testing.T) {
	var fired atomic.Int32
	lease := newStubLease(true)
	s := NewScheduler()
	s.WithLeaderElection(lease)
	if err := s.Register(CronJob{
		Name: "j", Spec: "* * * * *",
		Run: func(context.Context) error { fired.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if !s.runTick(context.Background(), time.Now()) {
		t.Error("leader runTick reported it did not fire")
	}
	// Jobs run in goroutines; the lease is released in a background goroutine
	// once they finish. Wait for both before asserting.
	s.inflight.Wait()
	<-lease.released
	if fired.Load() != 1 {
		t.Errorf("leader fired %d jobs, want 1", fired.Load())
	}
}

// TestRunTick_NoLeaderFiresAlways: without leader election configured, every
// replica fires every tick (the pre-HA single-instance behavior is unchanged).
func TestRunTick_NoLeaderFiresAlways(t *testing.T) {
	var fired atomic.Int32
	s := NewScheduler() // no WithLeaderElection
	if err := s.Register(CronJob{
		Name: "j", Spec: "* * * * *",
		Run: func(context.Context) error { fired.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if !s.runTick(context.Background(), time.Now()) {
		t.Error("no-leader runTick should fire")
	}
	s.inflight.Wait() // job runs in a goroutine; wait before asserting
	if fired.Load() != 1 {
		t.Errorf("fired %d jobs, want 1", fired.Load())
	}
}

// TestRunTick_DoesNotBlockOnInflightJobs pins the run-loop liveness contract:
// runTick must return promptly even while matching jobs are still running. The
// run loop selects on s.stop / ctx.Done() between ticks, and StopContext's
// deadline-bounded join is only reachable if the loop is NOT parked inside
// runTick. A blocking inflight.Wait() here would make a context-ignoring job
// hang StopContext forever (the documented SIGTERM scenario).
func TestRunTick_DoesNotBlockOnInflightJobs(t *testing.T) {
	s := NewScheduler()
	started := make(chan struct{})
	release := make(chan struct{})
	if err := s.Register(CronJob{
		Name: "slow", Spec: "* * * * *",
		Run: func(context.Context) error {
			close(started)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{}, 1)
	go func() {
		s.runTick(context.Background(), time.Now())
		close(done)
	}()
	<-started // the job is now running and blocked on release
	select {
	case <-done:
		// good: runTick returned even though the job is still running
	case <-time.After(time.Second):
		close(release)
		t.Fatal("runTick blocked on inflight.Wait() while a job is still running — run loop is not selectable, StopContext would hang")
	}
	close(release)
}

// TestRunTick_ReleasesLeaseOnRunOncePanic: a panic inside RunOnce (here, a
// panicking gate) must still schedule the lease release — the release is
// deferred, not run only after RunOnce returns. Without the defer the lease
// and its pinned connection would leak past the panic.
func TestRunTick_ReleasesLeaseOnRunOncePanic(t *testing.T) {
	lease := newStubLease(true)
	s := NewScheduler()
	s.WithLeaderElection(lease)
	s.SetGate(func(string) bool { panic("gate boom") })
	if err := s.Register(CronJob{
		Name: "j", Spec: "* * * * *",
		Run: func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }() // runTick propagates the gate panic
		s.runTick(context.Background(), time.Now())
	}()
	select {
	case <-lease.released:
		// good: lease released despite the panic
	case <-time.After(time.Second):
		t.Fatal("lease was not released after runTick panicked — release must be deferred")
	}
}
