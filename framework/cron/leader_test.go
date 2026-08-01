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
	released bool
}

func (l *stubLease) Acquire(ctx context.Context) (bool, func(), error) {
	if !l.held {
		return false, nil, nil
	}
	return true, func() { l.released = true }, nil
}

// TestRunTick_SkipsWhenNotLeader: when leader election is configured and this
// replica does NOT win the lease, the tick skips every job — so a second
// replica sharing the lease never double-fires the schedule.
func TestRunTick_SkipsWhenNotLeader(t *testing.T) {
	var fired atomic.Int32
	s := NewScheduler()
	s.WithLeaderElection(&stubLease{held: false})
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
	lease := &stubLease{held: true}
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
	if fired.Load() != 1 {
		t.Errorf("leader fired %d jobs, want 1", fired.Load())
	}
	if !lease.released {
		t.Error("lease was not released after firing")
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
	if fired.Load() != 1 {
		t.Errorf("fired %d jobs, want 1", fired.Load())
	}
}
