package processmoduletest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
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
// behavior driven by more env vars. See [processModuleChildMain].

// childEnvName is the env var gating the in-test child binary.
const childEnvName = "GOFASTR_PROCESS_MODULE_CHILD"

// childMode enumerates the behavior presets the child honors. The harness
// sets one via [childEnvName].
type childMode string

const (
	childModeEcho      childMode = "echo"       // happy path: handshake, ready, echo http
	childModeBadDigest childMode = "bad_digest" // surface_sha256 mismatch → terminal
	childModeSlow      childMode = "slow"       // http handler sleeps so kill -9 lands mid-call
	childModeCrashExit childMode = "crash_exit" // exit 1 immediately on each spawn
)

// buildChildArtifact compiles a tiny symlink to the running test binary so
// the runner's SHA-256 pin can verify it. Returns (path, sha256). The path
// IS os.Args[0]; we just hash it and pass the same binary as the artifact.
func buildChildArtifact(t *testing.T) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	// Copy to a scratch file so the test binary's path is stable across
	// re-invocations (os.Executable may point at a go-build temp path).
	dir := t.TempDir()
	dst := filepath.Join(dir, testExecutablePath("child"))
	in, err := os.Open(exe)
	if err != nil {
		t.Fatalf("open exe: %v", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create dst: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy: %v", err)
	}
	out.Close()
	if err := os.Chmod(dst, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	sha, err := sha256OfFile(dst)
	if err != nil {
		t.Fatalf("sha256: %v", err)
	}
	return dst, sha
}

// childEnv returns the env block for re-executing the test binary as a
// module child in the given mode. PATH is preserved so the child can exec.
func childEnv(mode childMode, extra map[string]string) []string {
	base := []string{
		childEnvName + "=" + string(mode),
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	for k, v := range extra {
		base = append(base, k+"="+v)
	}
	return base
}

// descriptorForChild builds a valid descriptor pinned to the test-binary
// artifact, with the given route + grants + surface digest.
func descriptorForChild(t *testing.T, mode childMode) framework.ProcessModuleDescriptor {
	t.Helper()
	path, sha := buildChildArtifact(t)
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
func newTestSupervisor(t *testing.T, store framework.ProcessModuleStore, mode childMode) *framework.ProcessModuleSupervisor {
	t.Helper()
	sup, err := framework.NewProcessModuleSupervisor(framework.SupervisorConfig{
		Store:             store,
		Runner:            &envRunner{inner: &framework.TrustedProcessRunner{}, env: childEnv(mode, nil)},
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
	d := descriptorForChild(t, childModeEcho)
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

// waitForState polls the supervisor's Info for name until state == want
// or the timeout elapses. Fatal on timeout.
func waitForState(t *testing.T, sup *framework.ProcessModuleSupervisor, name string, want framework.ProcessState, timeout time.Duration) {
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
	t.Fatalf("waitForState %q: want %s, last=%s", name, want, info.State)
}

// ---- Test 2: handshake digest mismatch → terminal Failed, NO restart ----

func TestSupervisor_HandshakeMismatchFailed(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		// Re-exec guard: the child runs [processModuleChildMain].
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, childModeBadDigest)
	d := descriptorForChild(t, childModeBadDigest)
	// Surface digest mismatch: the descriptor says X, the child echoes Y
	// (childModeBadDigest). The handshake must terminate the module.
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Must land in Failed (terminal), not loop Crashed → Backoff → Starting.
	waitForState(t, sup, d.Name, framework.StateFailed, 5*time.Second)
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
	if os.Getenv(childEnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, childModeCrashExit)
	d := descriptorForChild(t, childModeCrashExit)
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

// ---- Test 5: disabled → 404; enabled-but-down → 503 + Retry-After ----

func TestSupervisor_Disabled404_EnableDown503(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, childModeEcho)
	d := descriptorForChild(t, childModeEcho)
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
	waitForState(t, sup, d.Name, framework.StateReady, 5*time.Second)
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

// ============================================================================
// Child process (self-exec). The test binary re-execs itself with
// GOFASTR_PROCESS_MODULE_CHILD=<mode>; this main wires a moduleproto Peer
// over stdin/stdout and serves the configured behavior. Nothing is written
// to the repo tree, the artifact IS the test binary copied to t.TempDir().
// ============================================================================

// processModuleChildMain is invoked from TestMain when the env guard is set.
// It returns the exit code the process should use.
func processModuleChildMain(mode childMode) int {
	codec, err := moduleproto.NewCodec(os.Stdin, os.Stdout, moduleproto.DefaultMaxFrameBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child: codec:", err)
		return 1
	}
	peer := moduleproto.NewPeer(codec, moduleproto.RoleChild)
	// Handshake handler: echo expected, with per-mode digest behavior.
	if err := peer.Handle(moduleproto.MethodHandshake, func(_ context.Context, params json.RawMessage) (any, error) {
		var hp moduleproto.HandshakeParams
		_ = json.Unmarshal(params, &hp)
		surface := hp.Expected.SurfaceSHA256
		if mode == childModeBadDigest {
			surface = "DIFFERENT"
		}
		return moduleproto.HandshakeResult{
			Proto: moduleproto.ProtoRange{Min: 1, Max: 1},
			Identity: moduleproto.Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: surface,
			Features:      nil,
			Ready:         false,
		}, nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "child: register handshake:", err)
		return 1
	}
	// Ready handler.
	if err := peer.Handle(moduleproto.MethodReady, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.ReadyResult{Ready: true}, nil
	}); err != nil {
		return 1
	}
	// HTTP handler.
	if err := peer.Handle(moduleproto.MethodHTTP, func(_ context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.HTTPRequestParams
		_ = json.Unmarshal(params, &p)
		switch mode {
		case childModeSlow:
			// Sleep so a kill -9 lands mid-call.
			time.Sleep(2 * time.Second)
		}
		body, _ := json.Marshal(map[string]any{"route": p.RouteID, "ok": true})
		return moduleproto.HTTPResponseResult{
			Status:  http.StatusOK,
			Headers: map[string]string{"X-Child": "demo"},
			Body: moduleproto.HTTPResponseBody{
				Kind:  moduleproto.BodyKindJSON,
				Value: body,
			},
		}, nil
	}); err != nil {
		return 1
	}
	// Drain handler.
	_ = peer.Handle(moduleproto.MethodDrain, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.DrainResult{Inflight: 0}, nil
	})
	peer.Start()
	if mode == childModeCrashExit {
		// Exit immediately on spawn. The host's exit watcher catches it.
		time.Sleep(20 * time.Millisecond) // let the read loop start
		os.Exit(1)
	}
	<-peer.Done()
	return 0
}

// childTestMain is the TestMain entry that dispatches to the child when the
// env guard is set. It returns true if the child ran (and the test process
// should exit); false otherwise (normal test run).
func childTestMain(m *testing.M) bool {
	if mode := childMode(os.Getenv(childEnvName)); mode != "" {
		// This is a child re-exec. Run the child loop and exit.
		os.Exit(processModuleChildMain(mode))
	}
	return false
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
	if childTestMain(m) {
		return
	}
	os.Exit(m.Run())
}
