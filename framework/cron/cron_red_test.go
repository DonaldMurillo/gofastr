//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: host-supplied extension-point callbacks (gate, OnError) must be panic-isolated exactly as job Run is.
// Surfaces: cron.go:RunOnce (inline gate call under the lock), cron.go:runTick (inline OnError on the loop goroutine), cron.go:run (loop has no recover)
// Finding: a panicking gate or OnError escapes RunOnce/runTick unrecovered — the per-job defer/recover covers only j.Run, so driven from the run loop a panicking callback kills the process.
// Fix direction: wrap the gate and OnError invocations in the same defer/recover-and-route isolation the per-job goroutine already applies to j.Run.
package cron

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerRedGatePanicIsolated(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	// A panicking gate must not escape RunOnce: the panic belongs to the
	// scheduler's isolation boundary (routed to OnError), not to the caller.
	t.Run("gate", func(t *testing.T) {
		s := NewScheduler()
		routed := make(chan string, 1)
		s.OnError = func(name string, _ error) { routed <- name }
		if err := s.Register(CronJob{
			Name: "gated",
			Spec: "* * * * *",
			Run:  func(context.Context) error { return nil },
		}); err != nil {
			t.Fatal(err)
		}
		s.SetGate(func(string) bool { panic("gate boom") })

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: [cron-gate] gate panic escaped RunOnce unrecovered: %v", r)
			}
		}()
		s.RunOnce(context.Background(), now)

		select {
		case name := <-routed:
			if name != "gated" {
				t.Errorf("gate panic routed to OnError as job %q, want %q", name, "gated")
			}
		case <-time.After(2 * time.Second):
			t.Errorf("SECURITY: [cron-gate] gate panic was contained but never routed to OnError")
		}
	})

	// A panicking OnError must not escape runTick: it runs on the loop
	// goroutine in production, where an escape is process-fatal.
	t.Run("onerror", func(t *testing.T) {
		s := NewScheduler()
		s.OnError = func(string, error) { panic("onerror boom") }
		s.WithLeaderElection(acquireFailLease{})

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: [cron-onerror] OnError panic escaped runTick unrecovered: %v", r)
			}
		}()
		s.runTick(context.Background(), now)
	})
}

// acquireFailLease is a LeaderElection whose Acquire always errors, the path
// that drives runTick into its OnError call.
type acquireFailLease struct{}

func (acquireFailLease) Acquire(context.Context) (bool, func(), error) {
	return false, nil, errors.New("lease unavailable")
}

// ---------------------------------------------------------------------------
// Round 2: leader-election callback panics + OnError re-entry panic.
// ---------------------------------------------------------------------------

// Child-process guards for the two goroutine-fatality scenarios below
// (pattern: core/fanout subscriber_queue_security_test.go).
const (
	cronOnErrorChildEnv = "GOFASTR_TEST_CRON_ONERROR_PANIC_CHILD"
	cronReleaseChildEnv = "GOFASTR_TEST_CRON_RELEASE_PANIC_CHILD"
)

// TestCronRedOnErrorPanicInRecover
// Property: OnError is host-supplied; its panic while routing a job error must not be re-entered from the job goroutine's recover handler, where the chained second panic is process-fatal.
// Surfaces: cron.go RunOnce per-job goroutine — error path invokes OnError (cron.go:266-267), the recover handler then invokes OnError again (cron.go:260-263); that second panic escapes the goroutine.
// Finding: Run returns an error + OnError panics → recover catches the first panic → OnError re-invoked inside the handler → new panic escapes the job goroutine → child process death. Evidence: child stack "panic: onerror boom [recovered, repanicked]" with frames cron.go:267 (error-path OnError) → recover at cron.go:260 → OnError re-invoked at cron.go:262. Severity: production — OnError is a host callback wired into every app.
// Fix direction: do not re-enter OnError unguarded from the recover handler — wrap both OnError call sites in the goroutine with their own recover/route-to-log so one panicking callback cannot chain a fatal second panic.
// Proven by re-exec: the child drives the fatal scenario and must exit 0; today the chained panic kills the child, so the parent observes a crash exit — the red failing assertion.
func TestCronRedOnErrorPanicInRecover(t *testing.T) {
	if os.Getenv(cronOnErrorChildEnv) == "1" {
		cronOnErrorChild()
		return // unreachable; cronOnErrorChild exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestCronRedOnErrorPanicInRecover$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), cronOnErrorChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 10 {
			t.Fatalf("onerror child scenario contract broken (exit %d):\n%s", exitErr.ExitCode(), out)
		}
		t.Errorf("SECURITY: [cron-onerror] OnError re-entry panic escaped the job goroutine and killed the process: %v\n--- child output ---\n%s", err, out)
		return
	}
	if strings.Contains(string(out), "panic:") {
		t.Errorf("SECURITY: [cron-onerror] child survived but reported a panic:\n%s", out)
	}
}

// cronOnErrorChild is the child-side scenario. It never returns.
// Exit codes: 0 = chained panic contained, sibling job survived; >=10 =
// scenario contract broken; runtime crash (exit 2) = the chained OnError
// panic escaped the job goroutine (the finding).
func cronOnErrorChild() {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	s := NewScheduler()
	var onErrCalls atomic.Int32
	s.OnError = func(string, error) {
		onErrCalls.Add(1)
		panic("onerror boom")
	}
	if err := s.Register(CronJob{
		Name: "fails",
		Spec: "* * * * *",
		Run:  func(context.Context) error { return errors.New("job failed") },
	}); err != nil {
		os.Exit(10)
	}
	healthy := make(chan struct{})
	if err := s.Register(CronJob{
		Name: "healthy",
		Spec: "* * * * *",
		Run:  func(context.Context) error { close(healthy); return nil },
	}); err != nil {
		os.Exit(11)
	}

	// Fires the job goroutine whose recover handler chains the fatal.
	s.RunOnce(context.Background(), now)

	// Join the job goroutines. inflight.Done is deferred ahead of the
	// recover handler, so it runs even while the chained panic unwinds;
	// StopContext returns with the fatal already in flight.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.StopContext(ctx); err != nil {
		os.Exit(12)
	}

	// Give the chained fatal time to land; alive past this point means the
	// re-entry panic was contained.
	time.Sleep(300 * time.Millisecond)

	select {
	case <-healthy:
	default:
		os.Exit(13) // sibling job never ran
	}
	if n := onErrCalls.Load(); n < 1 {
		os.Exit(14) // error path never exercised
	}
	os.Exit(0)
}

// TestCronRedAcquirePanicIsolated
// Property: LeaderElection.Acquire is a host/driver-supplied callback (doc invites Redis/etcd implementations); its panic must be contained to the tick and routed like other scheduler faults.
// Surfaces: cron.go runTick — inline Acquire call at cron.go:155 runs on the loop goroutine; run() (cron.go:273) has no recover, so in production the escape is process-fatal.
// Finding: a panicking Acquire escapes runTick unrecovered and is never routed to OnError. Severity: production — custom lease implementations are a documented extension point.
// Fix direction: recover around the Acquire call in runTick, route the wrapped panic to OnError("(leader-election)", ...) and skip the tick.
func TestCronRedAcquirePanicIsolated(t *testing.T) {
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

// TestCronRedReleasePanicIsolated
// Property: the per-tick release callback is driver-supplied; its panic must be contained to the release goroutine and surfaced, not process-fatal.
// Surfaces: cron.go runTick — the deferred bare goroutine at cron.go:172-177 calls release() after inflight.Wait() with no recover anywhere between it and the goroutine top.
// Finding: Acquire returning (true, panicking release, nil) → once the tick's jobs finish, release() panics on an untracked goroutine → child process death. Evidence: child stack "panic: release boom" on runTick.func1.1 with the release() frame at cron.go:175. Severity: production — release wraps driver teardown (e.g. closing a pinned conn) in custom leases.
// Fix direction: wrap the release() call in that goroutine with recover and route the wrapped panic to OnError, keeping the lease-release scheduling intact.
// Proven by re-exec: the child drives runTick, syncs on the release invocation via a channel, and must exit 0; today the release panic kills the child — the red failing assertion.
func TestCronRedReleasePanicIsolated(t *testing.T) {
	if os.Getenv(cronReleaseChildEnv) == "1" {
		cronReleaseChild()
		return // unreachable; cronReleaseChild exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestCronRedReleasePanicIsolated$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), cronReleaseChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 10 {
			t.Fatalf("release child scenario contract broken (exit %d):\n%s", exitErr.ExitCode(), out)
		}
		t.Errorf("SECURITY: [cron-release] release panic escaped the untracked goroutine and killed the process: %v\n--- child output ---\n%s", err, out)
		return
	}
	if strings.Contains(string(out), "panic:") {
		t.Errorf("SECURITY: [cron-release] child survived but reported a panic:\n%s", out)
	}
}

// cronReleaseChild is the child-side scenario. It never returns.
// Exit codes: 0 = release panic contained and routed; >=10 = scenario
// contract broken; runtime crash (exit 2) = the release panic escaped the
// untracked goroutine (the finding).
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
	// runs in the deferred background goroutine (cron.go:172-177).
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
// always panics, driving runTick's deferred-goroutine path (cron.go:172-177).
type releaseFuncLease struct{ release func() }

func (l releaseFuncLease) Acquire(context.Context) (bool, func(), error) {
	return true, l.release, nil
}
