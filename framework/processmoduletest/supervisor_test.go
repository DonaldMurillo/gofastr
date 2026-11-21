package processmoduletest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/internal/pmtestkit"
)

// This file holds the wave-2a supervisor integration tests, split out of
// the framework root package so the root can run under the CI race gate
// (issue #208) without the spawn-heavy timing scenarios that flaked there
// under -race instrumentation. The scenarios pinned here:
//
//   - TestSupervisor_Disabled404_EnableDown503 (two-layer gate)
//   - TestSupervisor_HandshakeMismatchFailed  (terminal Failed, no restart)
//   - TestSupervisor_CircuitOpensAndGenResets (5 crashes/60s + gen bump)
//   - TestSupervisor_UntrustedNoSandbox       (constructor error)
//
// Three scenarios stayed in the framework root because they reach
// unexported supervisor internals (moduleSlot construction, Slot's child
// handle, the store's raw db) and exporting seams for them would be API
// that exists only for tests: KillMidCallBuffered503,
// StoreUnreachableDrains, RemoteToggleCrossReplica. The same blocks
// TestGate_ConvergenceAndRevoke in the gate file.
//
// The child process is the test binary itself, re-executed under an env
// guard (GOFASTR_PROCESS_MODULE_CHILD=…). The child wires a moduleproto
// Peer over stdin/stdout and serves a configurable handshake/ready/http
// behavior driven by more env vars. See [pmtestkit.ChildMain].

// descriptorForChild builds a valid descriptor pinned to the test-binary
// artifact, with the given route + grants + surface digest.
func descriptorForChild(t *testing.T, mode pmtestkit.Mode) framework.ProcessModuleDescriptor {
	t.Helper()
	path, sha := pmtestkit.ChildArtifact(t)
	surface, surfaceErr := framework.ComputeSurfaceSHA256(framework.ProcessModuleDescriptor{
		Name: "demo", Version: "1.0.0",
		Routes:          []framework.RouteDeclaration{{ID: "echo", Method: "GET", Path: "/echo"}},
		RequestedGrants: []access.Permission{"articles:read"},
		TrustTier:       framework.TrustTrusted,
	})
	if surfaceErr != nil {
		// A failure here yields the zero digest, which the handshake then
		// compares against the child's real one: the test would fail as a
		// digest mismatch and send the reader looking at the protocol.
		t.Fatalf("ComputeSurfaceSHA256: %v", surfaceErr)
	}
	d := framework.ProcessModuleDescriptor{
		Name:            "demo",
		Version:         "1.0.0",
		ArtifactPath:    path,
		ArtifactSHA256:  sha,
		SurfaceSHA256:   surface,
		Routes:          []framework.RouteDeclaration{{ID: "echo", Method: "GET", Path: "/echo"}},
		RequestedGrants: []access.Permission{"articles:read"},
		TrustTier:       framework.TrustTrusted,
	}
	return d
}

// newTestSupervisor constructs a supervisor with test-friendly knobs over
// the given store. The runner is wrapped in an [envRunner] so the child
// re-exec receives GOFASTR_PROCESS_MODULE_CHILD=<mode>.
func newTestSupervisor(t *testing.T, store framework.ProcessModuleStore, mode pmtestkit.Mode) *framework.ProcessModuleSupervisor {
	t.Helper()
	sup, err := framework.NewProcessModuleSupervisor(framework.SupervisorConfig{
		Store:             store,
		Runner:            &envRunner{inner: &framework.TrustedProcessRunner{}, env: pmtestkit.Env(mode, nil)},
		Broker:            framework.NopBroker{},
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

// newBareTestSupervisor constructs a supervisor with an explicit runner
// (no envRunner wrapping), used by tests that don't spawn real children
// (e.g. TestSupervisor_UntrustedNoSandbox, which fails at Register).
func newBareTestSupervisor(t *testing.T, store framework.ProcessModuleStore, runner framework.Runner) *framework.ProcessModuleSupervisor {
	t.Helper()
	sup, err := framework.NewProcessModuleSupervisor(framework.SupervisorConfig{
		Store:             store,
		Runner:            runner,
		Broker:            framework.NopBroker{},
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

// ---- Test 1: untrusted tier with no sandbox → constructor error ----

func TestSupervisor_UntrustedNoSandbox(t *testing.T) {
	store := newTestStore(t)
	sup := newBareTestSupervisor(t, store, &framework.TrustedProcessRunner{})
	d := descriptorForChild(t, pmtestkit.ModeEcho)
	d.TrustTier = framework.TrustUntrusted
	_, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"})
	if err == nil {
		t.Fatal("untrusted descriptor with no sandbox must fail Register")
	}
	if _, ok := errors.AsType[*framework.UntrustedNoSandboxError](err); !ok {
		t.Fatalf("want framework.UntrustedNoSandboxError, got %T(%v)", err, err)
	}
	// Module never reached Ready.
	if sl := sup.Slot(d.Name); sl != nil {
		t.Errorf("untrusted module should not be registered in a slot")
	}
}

// envRunner wraps a framework.Runner and appends fixed env entries to every
// framework.ChildSpec before delegating. Used by tests to pass the child-mode env
// var through the supervisor's spawn path without supervisor changes.
type envRunner struct {
	inner framework.Runner
	env   []string
}

func (r *envRunner) Start(ctx context.Context, spec framework.ChildSpec) (framework.RunningChild, error) {
	spec.ExtraEnv = append(append([]string{}, spec.ExtraEnv...), r.env...)
	return r.inner.Start(ctx, spec)
}

// ---- Test 2: handshake digest mismatch → terminal Failed, NO restart ----

func TestSupervisor_HandshakeMismatchFailed(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		// Re-exec guard: the child runs [pmtestkit.ChildMain].
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, pmtestkit.ModeBadDigest)
	d := descriptorForChild(t, pmtestkit.ModeBadDigest)
	// Surface digest mismatch: the descriptor says X, the child echoes Y
	// (pmtestkit.ModeBadDigest). The handshake must terminate the module.
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Must land in Failed (terminal), not loop Crashed → Backoff → Starting.
	waitForStateTB(t, sup, d.Name, framework.StateFailed, 5*time.Second)
	// Wait another spawn-deadline and confirm no restart attempt.
	time.Sleep(300 * time.Millisecond)
	info, _ := sup.Info(d.Name)
	if info.State != framework.StateFailed {
		t.Errorf("after wait: state = %s, want Failed (no restart)", info.State)
	}
	if info.RestartCount != 0 {
		t.Errorf("restart count = %d, want 0 (integrity faults do not charge)", info.RestartCount)
	}
}

// ---- Test 3: circuit opens after 5 crashes, generation bump resets ----

func TestSupervisor_CircuitOpensAndGenResets(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, pmtestkit.ModeCrashExit)
	d := descriptorForChild(t, pmtestkit.ModeCrashExit)
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// The child exits immediately on every spawn; the supervisor will
	// charge the circuit 5 times within CircuitWindow and open it.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := sup.Info(d.Name)
		if info.CircuitOpen {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	info, _ := sup.Info(d.Name)
	if !info.CircuitOpen {
		t.Fatalf("circuit should be open after 5 crashes; restarts=%d", info.RestartCount)
	}
	if info.RestartCount < 5 {
		t.Errorf("restart count = %d, want >= 5", info.RestartCount)
	}
	// Generation bump resets the circuit (design §8).
	if _, err := sup.BumpGeneration(context.Background(), d.Name); err != nil {
		t.Fatalf("bump: %v", err)
	}
	// Allow a reconcile pass.
	time.Sleep(100 * time.Millisecond)
	info, _ = sup.Info(d.Name)
	if info.CircuitOpen {
		t.Errorf("generation bump must reset circuit; still open (restarts=%d)", info.RestartCount)
	}
}

// A child that dies while the host's handshake call is in flight is a
// crash and charges the circuit like one. Before the fix the supervisor
// read the "handshake:"-wrapped transport error as an integrity fault and
// went terminal on the first attempt, which is why
// TestSupervisor_CircuitOpensAndGenResets flaked under load: its child
// sometimes exited before the handshake write rather than after it.
func TestSupervisor_ChildDyingMidHandshakeIsACrash(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, pmtestkit.ModeCrashInHandshake)
	d := descriptorForChild(t, pmtestkit.ModeCrashInHandshake)
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := sup.Info(d.Name)
		if info.CircuitOpen || info.State == framework.StateFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	info, _ := sup.Info(d.Name)
	if info.State == framework.StateFailed {
		t.Fatalf("a child dying mid-handshake went terminal (Failed) after %d restarts; want a crash cycle", info.RestartCount)
	}
	if !info.CircuitOpen || info.RestartCount < 5 {
		t.Fatalf("circuit open = %v, restarts = %d; want the circuit open after 5 crashes", info.CircuitOpen, info.RestartCount)
	}
}

// ---- Test 5: disabled → 404; enabled-but-down → 503 + Retry-After ----

func TestSupervisor_Disabled404_EnableDown503(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, pmtestkit.ModeEcho)
	d := descriptorForChild(t, pmtestkit.ModeEcho)
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	// Disabled (default): the route gate would 404. We cannot easily drive
	// the gate from this test (it lives in the router); we assert that the
	// supervisor's proxy is NOT reachable by checking state is
	// InstalledDisabled and Info reports disabled.
	info, _ := sup.Info(d.Name)
	if info.State != framework.StateInstalledDisabled {
		t.Errorf("freshly registered module state = %s, want InstalledDisabled", info.State)
	}

	// Enable, but the proxy hit during the Starting window must 503.
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Drive the proxy immediately, before Ready lands, to catch the
	// enabled-but-not-Ready 503 window (decision D). If the spawn is
	// too fast to observe, the test logs and continues.
	hit503 := false
	for range 10 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/echo", nil)
		sup.ProxyHandler(d.Name, "echo").ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			hit503 = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Wait for Ready, then verify a successful proxy call.
	waitForStateTB(t, sup, d.Name, framework.StateReady, 5*time.Second)
	okRec := httptest.NewRecorder()
	okReq := httptest.NewRequest(http.MethodGet, "/echo", nil)
	sup.ProxyHandler(d.Name, "echo").ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Errorf("ready proxy: status = %d, want 200", okRec.Code)
	}

	// Disable: drain the child. While DrainingDisable or after, proxy → 503.
	if err := sup.Disable(context.Background(), d.Name); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// Wait for the drain to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := sup.Info(d.Name)
		if info.State == framework.StateInstalledDisabled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	disabledRec := httptest.NewRecorder()
	disabledReq := httptest.NewRequest(http.MethodGet, "/echo", nil)
	sup.ProxyHandler(d.Name, "echo").ServeHTTP(disabledRec, disabledReq)
	// serveProxy is invoked AFTER the route gate would have 404'd. With
	// the gate bypassed (direct call), a not-Ready module returns 503.
	if disabledRec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled proxy: status = %d, want 503", disabledRec.Code)
	}
	if !hit503 {
		t.Logf("note: did not observe the enable-time 503 window (spawn was fast)")
	}
}

// TestMain dispatches two modes of this test binary:
//   - module child (GOFASTR_PROCESS_MODULE_CHILD=<mode>): wires a
//     moduleproto Peer for the supervisor integration tests.
//   - test runner: m.Run() (the default).
//
// Unlike the framework root package there is no sandbox-probe child
// dispatch here: the probe machinery is unexported in framework root, so
// the tests that spawn probe children stayed there.
func TestMain(m *testing.M) {
	if pmtestkit.MaybeChild() {
		return
	}
	os.Exit(m.Run())
}
