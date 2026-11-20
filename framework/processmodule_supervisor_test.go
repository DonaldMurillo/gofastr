package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/internal/pmtestkit"
)

// This file holds the wave-2a supervisor integration helpers and the
// scenarios that could NOT move to framework/processmoduletest for the
// CI race gate split (issue #208). The movable scenarios moved there;
// these stayed because each reaches unexported supervisor internals and
// exporting seams for them would be API that exists only for tests:
//
//   - TestSupervisor_KillMidCallBuffered503  (Slot's live child handle)
//   - TestSupervisor_StoreUnreachableDrains  (the store's raw db handle)
//   - TestSupervisor_RemoteToggleCrossReplica (moduleSlot construction)
//
// The helpers below (child artifact, env, descriptor, supervisors) are
// shared with the framework-root cover tests and with these scenarios.
//
// The child process is the test binary itself, re-executed under an env
// guard (GOFASTR_PROCESS_MODULE_CHILD=…). The child wires a moduleproto
// Peer over stdin/stdout and serves a configurable handshake/ready/http
// behavior driven by more env vars. See [pmtestkit.ChildMain].

// descriptorForChild builds a valid descriptor pinned to the test-binary
// artifact, with the given route + grants + surface digest.
func descriptorForChild(t *testing.T, mode pmtestkit.Mode) ProcessModuleDescriptor {
	t.Helper()
	path, sha := pmtestkit.ChildArtifact(t)
	surface, _ := ComputeSurfaceSHA256(ProcessModuleDescriptor{
		Name: "demo", Version: "1.0.0",
		Routes:          []RouteDeclaration{{ID: "echo", Method: "GET", Path: "/echo"}},
		RequestedGrants: []access.Permission{"articles:read"},
		TrustTier:       TrustTrusted,
	})
	d := ProcessModuleDescriptor{
		Name:            "demo",
		Version:         "1.0.0",
		ArtifactPath:    path,
		ArtifactSHA256:  sha,
		SurfaceSHA256:   surface,
		Routes:          []RouteDeclaration{{ID: "echo", Method: "GET", Path: "/echo"}},
		RequestedGrants: []access.Permission{"articles:read"},
		TrustTier:       TrustTrusted,
	}
	return d
}

// newTestSupervisor constructs a supervisor with test-friendly knobs over
// the given store. The runner is wrapped in an [envRunner] so the child
// re-exec receives GOFASTR_PROCESS_MODULE_CHILD=<mode>.
func newTestSupervisor(t *testing.T, store ProcessModuleStore, mode pmtestkit.Mode) *ProcessModuleSupervisor {
	t.Helper()
	return newTestSupervisorWith(t, store, mode, nil)
}

// newTestSupervisorWith is newTestSupervisor with tweak applied to the
// config before the supervisor is built.
func newTestSupervisorWith(t *testing.T, store ProcessModuleStore, mode pmtestkit.Mode, tweak func(*SupervisorConfig)) *ProcessModuleSupervisor {
	t.Helper()
	cfg := SupervisorConfig{
		Store:             store,
		Runner:            &envRunner{inner: &TrustedProcessRunner{}, env: pmtestkit.Env(mode, nil)},
		Broker:            NopBroker{},
		SpawnDeadline:     3 * time.Second,
		PollInterval:      50 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		LeaseTTL:          500 * time.Millisecond,
		DrainPerModule:    500 * time.Millisecond,
		BackoffMin:        5 * time.Millisecond,
		BackoffMax:        50 * time.Millisecond,
		CircuitThreshold:  5,
		CircuitWindow:     10 * time.Second,
		Logf:              safeLogf(t),
	}
	if tweak != nil {
		tweak(&cfg)
	}
	sup, err := NewProcessModuleSupervisor(cfg)
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sup.Close(ctx)
	})
	return sup
}

// newBareTestSupervisor constructs a supervisor with an explicit runner
// (no envRunner wrapping), used by tests that don't spawn real children
// (e.g. TestSupervisor_UntrustedNoSandbox, which fails at Register).
func newBareTestSupervisor(t *testing.T, store ProcessModuleStore, runner Runner) *ProcessModuleSupervisor {
	t.Helper()
	sup, err := NewProcessModuleSupervisor(SupervisorConfig{
		Store:             store,
		Runner:            runner,
		Broker:            NopBroker{},
		SpawnDeadline:     3 * time.Second,
		PollInterval:      50 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		LeaseTTL:          500 * time.Millisecond,
		DrainPerModule:    500 * time.Millisecond,
		BackoffMin:        5 * time.Millisecond,
		BackoffMax:        50 * time.Millisecond,
		CircuitThreshold:  5,
		CircuitWindow:     10 * time.Second,
		Logf:              safeLogf(t),
	})
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sup.Close(ctx)
	})
	return sup
}

// safeLogf wraps t.Logf so supervisor goroutines that outlive the test
// function body do not panic with "Log in goroutine after test has completed".
// The supervisor's spawn / drain / exit-watcher goroutines are intentionally
// fire-and-forget; this guard lets Close finish draining them without the
// test framework killing the process.
func safeLogf(t *testing.T) func(string, ...any) {
	var done atomic.Bool
	t.Cleanup(func() { done.Store(true) })
	return func(format string, args ...any) {
		if done.Load() {
			return
		}
		t.Logf(format, args...)
	}
}

// envRunner wraps a Runner and appends fixed env entries to every
// ChildSpec before delegating. Used by tests to pass the child-mode env
// var through the supervisor's spawn path without supervisor changes.
type envRunner struct {
	inner Runner
	env   []string
}

func (r *envRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	spec.ExtraEnv = append(append([]string{}, spec.ExtraEnv...), r.env...)
	return r.inner.Start(ctx, spec)
}

// ---- Test 4: kill -9 mid module.http → buffered 503, restart counted ----

func TestSupervisor_KillMidCallBuffered503(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, pmtestkit.ModeSlow)
	d := descriptorForChild(t, pmtestkit.ModeSlow)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForStateTB(t, sup, d.Name, StateReady, 5*time.Second)

	// Drive the proxy via an httptest.ResponseRecorder, but in a separate
	// goroutine, the child sleeps in module.http, so the call blocks.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sup.serveProxy(d.Name, "echo", rec, req)
	}()
	// Give the call a moment to land in the child.
	time.Sleep(100 * time.Millisecond)
	// Kill -9 the child mid-call.
	sl := sup.Slot(d.Name)
	sl.mu.RLock()
	child := sl.child
	sl.mu.RUnlock()
	if child == nil {
		t.Fatal("no running child to kill")
	}
	_ = child.Kill()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not return after child kill")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (buffered)", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on buffered 503")
	}
	// Supervisor should restart and the host must stay healthy.
	waitForStateTB(t, sup, d.Name, StateReady, 5*time.Second)
}

// ---- Test 6: store unreachable past lease TTL → drained, 503 ----

func TestSupervisor_StoreUnreachableDrains(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, pmtestkit.ModeEcho)
	d := descriptorForChild(t, pmtestkit.ModeEcho)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForStateTB(t, sup, d.Name, StateReady, 5*time.Second)

	// Close the underlying DB so the store becomes unreadable.
	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	// Wait past the lease TTL for the fail-closed path to fire.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := sup.Info(d.Name)
		if info.LeaseFailing {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	info, _ := sup.Info(d.Name)
	if !info.LeaseFailing {
		t.Fatalf("lease should be failing after store close + TTL")
	}
	// Proxy must return 503 (state not Ready or leaseFailing).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	sup.serveProxy(d.Name, "echo", rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("lease-failed proxy: status = %d, want 503", rec.Code)
	}
}

// ---- Test 7: remote toggle on a second replica (shared store) ----

func TestSupervisor_RemoteToggleCrossReplica(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	store := newTestStore(t)
	// Two supervisors over the SAME store, distinct replica IDs.
	sup1 := newTestSupervisor(t, store, pmtestkit.ModeEcho)
	sup2, err := NewProcessModuleSupervisor(SupervisorConfig{
		Store:             store,
		Runner:            &envRunner{inner: &TrustedProcessRunner{}, env: pmtestkit.Env(pmtestkit.ModeEcho, nil)},
		Broker:            NopBroker{},
		ReplicaID:         "replica-2",
		SpawnDeadline:     3 * time.Second,
		PollInterval:      50 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		LeaseTTL:          500 * time.Millisecond,
		DrainPerModule:    500 * time.Millisecond,
		BackoffMin:        5 * time.Millisecond,
		BackoffMax:        50 * time.Millisecond,
		CircuitThreshold:  5,
		CircuitWindow:     10 * time.Second,
		Logf:              safeLogf(t),
	})
	if err != nil {
		t.Fatalf("new sup2: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sup2.Close(ctx)
	})

	d := descriptorForChild(t, pmtestkit.ModeEcho)
	// Register on sup1 (creates the desired row at gen 1, disabled).
	if _, err := sup1.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	// sup2 registers the same descriptor against the shared store: the
	// row exists and matches, so sup2 adopts it unchanged and supervises
	// independently against it.
	if res, err := sup2.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("sup2 Register: %v", err)
	} else if res.Outcome != InstallUnchanged {
		t.Fatalf("sup2 Register outcome = %s, want unchanged", res.Outcome)
	}

	sup1.StartLoops()
	sup2.StartLoops()

	// Enable on sup1: bumps desired state to enabled. sup2's periodic poll
	// observes the higher enabled state and spawns its own child.
	if err := sup1.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("sup1 enable: %v", err)
	}
	waitForStateTB(t, sup1, d.Name, StateReady, 5*time.Second)
	// sup2 converges via periodic poll.
	waitForStateOn(t, sup2, d.Name, StateReady, 5*time.Second)

	// Disable on sup1: sup2 observes via poll and drains.
	if err := sup1.Disable(context.Background(), d.Name); err != nil {
		t.Fatalf("sup1 disable: %v", err)
	}
	waitForStateOn(t, sup2, d.Name, StateInstalledDisabled, 5*time.Second)
}

// waitForStateOn polls like waitForStateTB and includes the supervisor in its timeout diagnostic.
func waitForStateOn(t *testing.T, sup *ProcessModuleSupervisor, name string, want ProcessState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := sup.Info(name)
		if err == nil && info.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, _ := sup.Info(name)
	t.Fatalf("waitForStateOn %p %q: want %s, last=%s", sup, name, want, info.State)
}

// TestMain dispatches two modes of the test binary:
//   - module child (GOFASTR_PROCESS_MODULE_CHILD=<mode>): wires a
//     moduleproto Peer for the supervisor integration tests.
//   - test runner: m.Run() (the default).
//
// The third mode, the sandbox probe child (GOFASTR_SANDBOX_PROBE=<id>),
// never reaches TestMain: the package init in processmodule_probe_child.go
// runs it and exits first, the same dispatch a host app binary gets.
func TestMain(m *testing.M) {
	if pmtestkit.MaybeChild() {
		return
	}
	os.Exit(m.Run())
}
