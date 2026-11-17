package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestShortcutsExpandToTheirSpecs(t *testing.T) {
	for short, spec := range map[string]string{
		"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *",
		"@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *", "@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *",
	} {
		got, err := ParseCron(short)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := ParseCron(spec)
		if got != want {
			t.Errorf("%s parsed to %+v, want %s (%+v)", short, got, spec, want)
		}
	}
}

func TestPlainLeaseTickReturnsWhileItsJobRuns(t *testing.T) {
	s := NewScheduler()
	lease := newStubLease(true)
	s.WithLeaderElection(lease)
	started, release := make(chan struct{}), make(chan struct{})
	if err := s.Register(CronJob{Name: "slow", Spec: "* * * * *", Run: func(context.Context) error {
		close(started)
		<-release
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.runTick(context.Background(), time.Now()); close(done) }()
	<-started
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("runTick under a plain lease waited for its job before returning")
	}
	select {
	case <-lease.released:
		t.Fatal("plain lease released while the tick's job still runs")
	default:
	}
	close(release)
	select {
	case <-lease.released:
	case <-time.After(time.Second):
		t.Fatal("plain lease not released after the tick's job ended")
	}
}

func TestGateSkipsJobOnceJobsAreJoined(t *testing.T) {
	s := NewScheduler()
	var ran atomic.Bool
	if err := s.Register(CronJob{Name: "gated", Spec: "* * * * *", Run: func(context.Context) error {
		ran.Store(true)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	s.SetGate(func(name string) bool { return name != "gated" })
	s.RunOnce(context.Background(), time.Now())
	s.Stop()
	if ran.Load() {
		t.Fatal("a job its gate refused ran")
	}
}
