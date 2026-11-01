package framework

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// firstHeldRunner holds its FIRST Start until gate closes; every later
// Start passes straight through. It records each spawn's instance id and
// the first spawn's outcome, so a test can tell the two spawns apart.
type firstHeldRunner struct {
	inner   Runner
	entered chan struct{}
	gate    chan struct{}
	// ignoreCancel runs the held Start on a context the spawn's cancel
	// cannot reach, the way a runner that does not watch ctx behaves.
	ignoreCancel bool

	mu         sync.Mutex
	calls      int
	instances  []string
	firstChild RunningChild
	firstErr   error
	firstDone  chan struct{}
}

func (r *firstHeldRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	r.mu.Lock()
	r.calls++
	first := r.calls == 1
	r.instances = append(r.instances, spec.InstanceID)
	r.mu.Unlock()
	if !first {
		return r.inner.Start(ctx, spec)
	}
	close(r.entered)
	<-r.gate
	if r.ignoreCancel {
		ctx = context.WithoutCancel(ctx)
	}
	c, err := r.inner.Start(ctx, spec)
	r.mu.Lock()
	r.firstChild, r.firstErr = c, err
	r.mu.Unlock()
	close(r.firstDone)
	return c, err
}

// Property (2026-10-31 bug hunt, O41): a spawn still starting when its
// generation is superseded never installs. A grant revoke while the
// first spawn was in Start drained a slot that had no child yet and
// spawned a replacement beside the pending one; the first spawn then
// finished, overwrote the slot's child and peer with its own, and kept
// serving reverse calls under the revoked grant set while the
// replacement ran orphaned.
func TestSupersededSpawnNeverInstalls(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	t.Run("runner honors cancel", func(t *testing.T) { supersededSpawnCase(t, false) })
	t.Run("runner ignores cancel", func(t *testing.T) { supersededSpawnCase(t, true) })
}

func supersededSpawnCase(t *testing.T, ignoreCancel bool) {
	ctx := context.Background()
	store := newTestStore(t)
	d := descriptorForChild(t, childModeEcho)
	runner := &firstHeldRunner{
		inner:        &envRunner{inner: &TrustedProcessRunner{}, env: childEnv(childModeEcho, nil)},
		entered:      make(chan struct{}),
		gate:         make(chan struct{}),
		firstDone:    make(chan struct{}),
		ignoreCancel: ignoreCancel,
	}
	sup := newBareTestSupervisor(t, store, runner)
	// Long enough that the held spawn never times out on its own: the
	// test is about the supersede, not the spawn deadline.
	sup.cfg.SpawnDeadline = 30 * time.Second
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(ctx, d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	select {
	case <-runner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first spawn never reached the runner")
	}

	// Revoke while the first spawn is held: the generation moves on and
	// the slot spawns again for it.
	if _, err := sup.RevokeGrants(ctx, d.Name, []access.Permission{"articles:read"}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 10*time.Second)
	runner.mu.Lock()
	if len(runner.instances) < 2 {
		runner.mu.Unlock()
		t.Fatal("the revoke did not spawn a replacement")
	}
	second := runner.instances[1]
	runner.mu.Unlock()

	close(runner.gate)
	select {
	case <-runner.firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first spawn never returned from the runner")
	}
	runner.mu.Lock()
	firstChild, firstErr := runner.firstChild, runner.firstErr
	runner.mu.Unlock()
	if !ignoreCancel && firstErr == nil {
		t.Error("the drain did not cancel the superseded spawn's context")
	}
	if firstErr == nil {
		exited := make(chan struct{})
		go func() { _ = firstChild.Wait(); close(exited) }()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = firstChild.Kill()
			t.Fatal("SECURITY: [processmodule-stale-spawn] the superseded spawn's child is still running")
		}
	}
	// Give a stale install the time it would need to land.
	time.Sleep(300 * time.Millisecond)
	sl := sup.Slot(d.Name)
	sl.mu.RLock()
	instance, state := sl.instanceID, sl.state
	sl.mu.RUnlock()
	if instance != second {
		t.Fatalf("SECURITY: [processmodule-stale-spawn] the slot holds instance %q, want the replacement %q", instance, second)
	}
	if state != StateReady {
		t.Fatalf("the superseded spawn moved the slot to %s, want Ready", state)
	}
}
