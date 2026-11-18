package cron

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/reexectest"
)

// Leader-election callback panic isolation: LeaderElection.Acquire and
// the per-tick release it returns are driver-supplied callbacks (custom
// Redis/etcd leases are a documented extension point) running on the
// tick loop / its deferred release goroutine, where an escape is
// process-fatal. Both run under the same recover-and-route-to-OnError
// net the per-job path gives j.Run.

const cronReleaseChildEnv = "GOFASTR_TEST_CRON_RELEASE_PANIC_CHILD"

// TestSchedulerAcquirePanicIsolated: a panicking Acquire is contained to
// the tick and routed to OnError like every other scheduler fault, never
// escaping runTick.
func TestSchedulerAcquirePanicIsolated(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	s := NewScheduler()
	routed := make(chan error, 1)
	s.OnError = func(_ string, err error) { routed <- err }
	s.WithLeaderElection(panicAcquireLease{})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [cron-acquire] Acquire panic escaped runTick unrecovered: %v", r)
		}
	}()
	s.runTick(context.Background(), now)

	select {
	case err := <-routed:
		if err == nil {
			t.Errorf("OnError routed a nil error for the Acquire panic")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("SECURITY: [cron-acquire] Acquire panic contained but never routed to OnError")
	}
}

// TestSchedulerReleasePanicIsolated: the per-tick release callback's
// panic is contained to the release goroutine and surfaced via OnError,
// not process-fatal. Proven by re-exec: the child drives runTick, syncs
// on the release invocation via a channel, and must exit 0.
func TestSchedulerReleasePanicIsolated(t *testing.T) {
	if os.Getenv(cronReleaseChildEnv) == "1" {
		cronReleaseChild()
		return // unreachable; cronReleaseChild exits
	}

	reexectest.RunChild(t, "TestSchedulerReleasePanicIsolated", cronReleaseChildEnv,
		"[cron-release]",
		"[cron-release] release panic escaped the untracked goroutine and killed the process")
}

// cronReleaseChild is the child-side scenario. It never returns.
// Exit codes: 0 = release panic contained and routed; >=10 = scenario
// contract broken; runtime crash (exit 2) = the release panic escaped
// the untracked goroutine (the finding).
func cronReleaseChild() {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	s := NewScheduler()
	routed := make(chan error, 1)
	s.OnError = func(_ string, err error) { routed <- err }

	releaseEntered := make(chan struct{})
	var once sync.Once
	s.WithLeaderElection(releaseFuncLease{release: func() {
		once.Do(func() { close(releaseEntered) })
		panic("release boom")
	}})
	if err := s.Register(CronJob{
		Name: "tick",
		Spec: "* * * * *",
		Run:  func(context.Context) error { return nil },
	}); err != nil {
		os.Exit(10)
	}

	// runTick must return normally on the caller's goroutine; the release
	// runs in the deferred background goroutine.
	if !s.runTick(context.Background(), now) {
		os.Exit(11)
	}

	select {
	case <-releaseEntered:
	case <-time.After(2 * time.Second):
		os.Exit(12) // release never invoked; surface not exercised
	}

	// Give the fatal time to land; alive past this point means the release
	// panic was contained.
	time.Sleep(300 * time.Millisecond)

	// Contained world must also surface the fault, not swallow it.
	select {
	case err := <-routed:
		if err == nil {
			os.Exit(13)
		}
	case <-time.After(2 * time.Second):
		os.Exit(14) // contained but never routed to OnError
	}
	os.Exit(0)
}

// panicAcquireLease is a LeaderElection whose Acquire always panics.
type panicAcquireLease struct{}

func (panicAcquireLease) Acquire(context.Context) (bool, func(), error) {
	panic("acquire boom")
}

// releaseFuncLease acquires successfully and hands back a release that
// always panics, driving runTick's deferred-goroutine path.
type releaseFuncLease struct{ release func() }

func (l releaseFuncLease) Acquire(context.Context) (bool, func(), error) {
	return true, l.release, nil
}
