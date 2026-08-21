package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// This file holds the §10 go/no-go gate-suite helpers and the ONE gate
// test that could not move to framework/processmoduletest for the CI race
// gate split (issue #208). The helpers (demo binary build, descriptor,
// supervisor knobs, capability-broker env) stayed because
// processmodule_bench_test.go builds on them; TestGate_ConvergenceAndRevoke
// stayed because it hand-constructs a moduleSlot for the second replica,
// which is unexported supervisor state. Everything else moved.
//
// The suite drives the REAL demo module binary (examples/processmodule-demo)
// against the REAL supervisor + store + broker — not a re-exec'd test
// binary. The demo is built into a temp dir once per test run; every test
// spawns it as a genuine out-of-process child over moduleproto/stdio.

// ---- demo binary (built once, shared across the run) ---------------------

var (
	demoBinOnce sync.Once
	demoBinPath string
	demoBinErr  error
)

// buildDemoBinary builds ./examples/processmodule-demo into a process-private
// temp dir once per test binary and returns (path, sha256). The build output
// is intentionally NOT tied to any one test's t.TempDir (sync.Once may fire in
// test A while test B later reuses the path); it lives for the run.
func buildDemoBinary(t testing.TB) (string, string) {
	t.Helper()
	demoBinOnce.Do(func() {
		gomod, err := exec.Command("go", "env", "GOMOD").Output()
		if err != nil || len(strings.TrimSpace(string(gomod))) == 0 {
			demoBinErr = fmt.Errorf("go env GOMOD: %v", err)
			return
		}
		repoRoot := filepath.Dir(strings.TrimSpace(string(gomod)))
		dir, err := os.MkdirTemp("", "gofastr-demobuild-*")
		if err != nil {
			demoBinErr = fmt.Errorf("mkdtemp: %w", err)
			return
		}
		bin := filepath.Join(dir, testExecutablePath("demo"))
		cmd := exec.Command("go", "build", "-o", bin, "./examples/processmodule-demo")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			demoBinErr = fmt.Errorf("go build demo: %v\n%s", err, out)
			return
		}
		sha, err := sha256OfFile(bin)
		if err != nil {
			demoBinErr = fmt.Errorf("sha256 demo: %w", err)
			return
		}
		demoBinPath = bin
		// Stash the sha on the package var via a closure-captured trick: we
		// re-hash on every call (cheap, ~MB) so the value is always fresh
		// and we avoid a second package var. buildDemoBinary returns it.
		_ = sha
	})
	if demoBinErr != nil {
		t.Fatalf("build demo binary: %v", demoBinErr)
	}
	sha, err := sha256OfFile(demoBinPath)
	if err != nil {
		t.Fatalf("sha256 demo: %v", err)
	}
	return demoBinPath, sha
}

// demoPingTool mirrors examples/processmodule-demo/main.go's pingTool EXACTLY.
// The handshake byte-compares ModuleToolDigest(this) against the descriptor's
// ToolDigest.SHA256, so the two definitions must not drift. If you change the
// demo's tool, change this too.
func demoPingTool() moduleproto.Tool {
	return moduleproto.Tool{
		ID:          "ping",
		Name:        "module.demo.ping",
		Description: "Reverse-queries the granted host entity and reports the row count.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	}
}

// demoDescriptor builds a valid descriptor pinned to the built demo binary,
// with the demo's canonical routes + the ping tool surface. The caller picks
// the grants + migration group + trust tier per gate item.
func demoDescriptor(t testing.TB, grants []access.Permission, migrationGroup string, tier TrustTier) ProcessModuleDescriptor {
	t.Helper()
	path, sha := buildDemoBinary(t)
	routes := []RouteDeclaration{
		{ID: "hello", Method: "GET", Path: "/demo/hello"},
		{ID: "tree", Method: "GET", Path: "/demo/tree"},
		{ID: "items", Method: "GET", Path: "/demo/items"},
		{ID: "refresh", Method: "POST", Path: "/demo/refresh"},
	}
	tools := []ToolDigest{{ID: "ping", SHA256: ModuleToolDigest(demoPingTool())}}
	d := ProcessModuleDescriptor{
		Name:            "demo",
		Version:         "1.0.0",
		ArtifactPath:    path,
		ArtifactSHA256:  sha,
		Routes:          routes,
		Tools:           tools,
		RequestedGrants: grants,
		TrustTier:       tier,
		MigrationGroup:  migrationGroup,
	}
	surface, err := ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatalf("compute surface sha: %v", err)
	}
	d.SurfaceSHA256 = surface
	return d
}

// demoRunner spawns the demo binary under TrustedProcessRunner, injecting the
// given DEMO_* env knobs via ChildSpec.ExtraEnv. It is the gate-test analog of
// supervisor_test.go's envRunner, but the artifact is the real demo binary
// (not a re-exec of the test binary).
type demoRunner struct {
	env map[string]string
}

func (r *demoRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	for k, v := range r.env {
		spec.ExtraEnv = append(spec.ExtraEnv, k+"="+v)
	}
	return (&TrustedProcessRunner{}).Start(ctx, spec)
}

// newGateSupervisor builds a test-friendly supervisor with the given store,
// broker, runner, and replica id. Knobs mirror newTestSupervisor so the
// poll/lease/backoff/circuit timings stay compressed.
func newGateSupervisor(t testing.TB, store ProcessModuleStore, broker ReverseBroker, runner Runner, replicaID string) *ProcessModuleSupervisor {
	t.Helper()
	sup, err := NewProcessModuleSupervisor(SupervisorConfig{
		Store:             store,
		Runner:            runner,
		Broker:            broker,
		ReplicaID:         replicaID,
		SpawnDeadline:     5 * time.Second,
		PollInterval:      50 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		LeaseTTL:          500 * time.Millisecond,
		DrainPerModule:    500 * time.Millisecond,
		BackoffMin:        5 * time.Millisecond,
		BackoffMax:        50 * time.Millisecond,
		CircuitThreshold:  5,
		CircuitWindow:     10 * time.Second,
		Logf:              gateSafeLogf(t),
	})
	if err != nil {
		t.Fatalf("new gate supervisor: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sup.Close(ctx)
	})
	return sup
}

// registerEnableReady is the common bring-up: register the demo, start the
// loops, enable, and wait for Ready.
func registerEnableReady(t testing.TB, sup *ProcessModuleSupervisor, d ProcessModuleDescriptor, approved ApprovedGrants) {
	t.Helper()
	if _, err := sup.Register(context.Background(), d, approved); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForStateTB(t, sup, d.Name, StateReady, 6*time.Second)
}

// gateSafeLogf is the testing.TB analog of supervisor_test.go's safeLogf: it
// mutes supervisor goroutines that log after the test completes (the spawn /
// drain / exit-watcher loops are fire-and-forget and outlive the test body).
func gateSafeLogf(tb testing.TB) func(string, ...any) {
	var done atomic.Bool
	tb.Cleanup(func() { done.Store(true) })
	return func(format string, args ...any) {
		if done.Load() {
			return
		}
		tb.Logf(format, args...)
	}
}

// waitForStateTB is the testing.TB analog of waitForState (which takes
// *testing.T). Shared by the gate helpers so they work under both T and B.
func waitForStateTB(tb testing.TB, sup *ProcessModuleSupervisor, name string, want ProcessState, timeout time.Duration) {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := sup.Info(name)
		if err == nil && info.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, _ := sup.Info(name)
	tb.Fatalf("waitForStateTB %q: want %s, last=%s", name, want, info.State)
}

// proxyGet drives the supervisor's proxy for one routeID and returns the
// recorder. cookie is optional; "" → no Cookie header.
func proxyGet(t testing.TB, sup *ProcessModuleSupervisor, name, routeID, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/"+routeID, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	sup.serveProxy(name, routeID, rec, req)
	return rec
}

// =====================================================================
// Item 1 — Crash containment (design §10.1)
// =====================================================================

// =====================================================================
// Capability-broker environment (shared by items 2 + 6)
// =====================================================================

// gateCapEnv is the real CRUD + capability-broker environment for the
// capability gate (item 2) and the convergence/revoke gate (item 6). It wires
// two owner-scoped entities (articles, secrets) over one SQLite DB, registers
// CRUD routes behind an auth middleware that resolves Cookie "sid=<user>" →
// user (so RequireOwner filters rows to the caller's owner id), and exposes a
// fresh-Broker factory so each supervisor gets its own broker over the shared
// data. It reuses the broker-test helpers (brokerEntity / brokerAuthMiddleware
// / …) so the wiring stays identical to processmodule_broker_test.go.
type gateCapEnv struct {
	db       *sql.DB
	router   http.Handler
	registry *Registry
	policy   *access.RolePolicy
}

// newGateCapEnv builds the env and applies the seed DDL. Rows are seeded per
// test (different gate items want different owner distributions).
func newGateCapEnv(t *testing.T) *gateCapEnv {
	t.Helper()
	// Register the global owner extractor so RequireOwner (owner.Get) can
	// resolve the Cookie-derived brokerTestUser to an owner id — the same
	// setup newCrudBrokerEnv uses in processmodule_broker_test.go.
	brokerInstallOwnerExtractor(t)
	db := brokerSetupDB(t,
		`CREATE TABLE articles (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, subject TEXT);`+
			`CREATE TABLE secrets (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, subject TEXT);`)
	articles := brokerEntity("articles", "articles", nil)
	secrets := brokerEntity("secrets", "secrets", nil)
	articles.SetDB(db)
	secrets.SetDB(db)
	reg := brokerRegistry(articles, secrets)
	artCH := crud.NewCrudHandler(articles, db)
	artCH.Registry = reg
	artCH.JSONCase = crud.CaseSnake
	secCH := crud.NewCrudHandler(secrets, db)
	secCH.Registry = reg
	secCH.JSONCase = crud.CaseSnake
	inner := router.New()
	crud.RegisterCrudRoutes(inner, artCH, "/articles", crud.CrudRouteOptions{NoLLMMD: true})
	crud.RegisterCrudRoutes(inner, secCH, "/secrets", crud.CrudRouteOptions{NoLLMMD: true})
	policy := access.NewRolePolicy()
	wrapped := brokerAuthMiddleware(policy, nil)(inner)
	return &gateCapEnv{db: db, router: wrapped, registry: reg, policy: policy}
}

// newBroker returns a fresh capability Broker over the shared router /
// registry / policy. Each supervisor gets its own (the handle table is
// per-broker; the data + routes are shared).
func (e *gateCapEnv) newBroker() *Broker {
	return NewBroker(e.router, e.registry, nil, "", WithBrokerPolicy(e.policy))
}

// =====================================================================
// Item 6 — Convergence + revoke (design §10.6)
// =====================================================================

// TestGate_ConvergenceAndRevoke proves, with two supervisors sharing one
// store, that a grant revoke on replica A bumps desired_generation and every
// other replica observes it within the poll bound — and that the revoked
// capability's next reverse call on the other replica is DENIED. This is the
// headline convergence property: revoke takes effect on the next reverse
// call after the replica reconciles, no token-expiry window.
func TestGate_ConvergenceAndRevoke(t *testing.T) {
	env := newGateCapEnv(t)
	brokerSeedRow(t, env.db, "articles", "a1", "userA", "Alpha-one")

	store := newTestStore(t)
	supA := newGateSupervisor(t, store, env.newBroker(), &demoRunner{env: map[string]string{
		"DEMO_QUERY_ENTITY": "articles",
	}}, "replica-A")
	supB := newGateSupervisor(t, store, env.newBroker(), &demoRunner{env: map[string]string{
		"DEMO_QUERY_ENTITY": "articles",
	}}, "replica-B")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", TrustTrusted)

	ctx := context.Background()
	// Register on A (installs the desired row at gen 1, disabled).
	if _, err := supA.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("supA register: %v", err)
	}
	// B registers the same descriptor against the shared store → the row
	// already exists (ErrModuleInstalled). Cross-replica, each replica
	// supervises independently against the shared row, so create B's slot
	// directly (same pattern as TestSupervisor_RemoteToggleCrossReplica).
	if _, err := supB.Register(ctx, d, ApprovedGrants{"articles:read"}); !errors.Is(err, ErrModuleInstalled) {
		t.Fatalf("supB register: want ErrModuleInstalled, got %v", err)
	}
	selectedRunner, selErr := SelectRunner(d.TrustTier, supB.runner, supB.sandbox)
	if selErr != nil {
		t.Fatalf("supB select runner: %v", selErr)
	}
	supB.mu.Lock()
	if supB.slots[d.Name] == nil {
		supB.slots[d.Name] = &moduleSlot{
			name: d.Name, desc: d, sup: supB,
			runner: selectedRunner,
			wake:   make(chan struct{}, 1),
			done:   make(chan struct{}),
		}
	}
	supB.mu.Unlock()

	supA.StartLoops()
	supB.StartLoops()
	if err := supA.Enable(ctx, d.Name); err != nil {
		t.Fatalf("supA enable: %v", err)
	}
	waitForState(t, supA, d.Name, StateReady, 6*time.Second)
	waitForStateOn(t, supB, d.Name, StateReady, 6*time.Second)

	// Pre-revoke: /items on B (as userA) succeeds — articles:read is granted.
	pre := proxyGet(t, supB, d.Name, "items", "sid=userA")
	if pre.Code != http.StatusOK {
		t.Fatalf("pre-revoke /items on B: status = %d, want 200", pre.Code)
	}

	// Revoke articles:read on A → generation bumps in the shared store.
	start := time.Now()
	newGen, err := supA.RevokeGrants(ctx, d.Name, nil)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if newGen < 2 {
		t.Fatalf("revoke did not bump generation: newGen=%d", newGen)
	}

	// ---- Convergence that WORKS end to end ----
	// B's periodic poll reads the shared store and observes the higher
	// desired_generation. The poll interval (50ms) + spawn time is the
	// convergence upper bound; assert B's ObservedGeneration reaches the
	// bumped value well within it.
	converged := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := supB.Info(d.Name)
		if info.ObservedGeneration >= newGen && info.State == StateReady {
			converged = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !converged {
		info, _ := supB.Info(d.Name)
		t.Fatalf("B did not observe generation %d (last observed=%d state=%s)", newGen, info.ObservedGeneration, info.State)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("generation convergence took %v (poll bound is 50ms + spawn); want well under 3s", elapsed)
	}

	// The store row is revoked (EffectiveGrants=[]) and B restarted at the new
	// generation. finishDrain re-reads the store's authoritative
	// EffectiveGrants on the upgrade respawn, so the narrowed set reaches the
	// new child's broker view — the reverse-call denial is proven end-to-end
	// by TestGate_RevokeDeniesReverseCall.
	desired, _ := store.GetDesired(ctx, d.Name)
	if len(desired.EffectiveGrants) != 0 {
		t.Fatalf("store EffectiveGrants after revoke = %v, want [] (revoke must clear the store row)", desired.EffectiveGrants)
	}
	// The reverse-call denial after revoke is proven end-to-end by
	// TestGate_RevokeDeniesReverseCall (single replica, the same store-backed
	// respawn path). Here we assert only the cross-replica convergence half.
}
