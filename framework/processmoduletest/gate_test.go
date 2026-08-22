package processmoduletest

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

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// This file is the §10 go/no-go gate suite (design #37, wave 4b), split
// out of the framework root package so the root can run under the CI race
// gate (issue #208). It drives the REAL demo module binary
// (examples/processmodule-demo) against the REAL supervisor + store +
// broker, not a re-exec'd test binary. The demo is built into a temp dir
// once per test run; every test spawns it as a genuine out-of-process
// child over moduleproto/stdio.
//
// Each gate item is pinned by one or more named TestGate_* tests. Items whose
// internals are already unit-proven by earlier waves are asserted-wired here
// via a single end-to-end path through the running child, not re-proved from
// scratch (design §10: "a gate the earlier waves already proved at unit level
// should ASSERT-IT'S-WIRED here, not re-prove the internals").

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
// The handshake byte-compares framework.ModuleToolDigest(this) against the descriptor's
// framework.ToolDigest.SHA256, so the two definitions must not drift. If you change the
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
func demoDescriptor(t testing.TB, grants []access.Permission, migrationGroup string, tier framework.TrustTier) framework.ProcessModuleDescriptor {
	t.Helper()
	path, sha := buildDemoBinary(t)
	routes := []framework.RouteDeclaration{
		{ID: "hello", Method: "GET", Path: "/demo/hello"},
		{ID: "tree", Method: "GET", Path: "/demo/tree"},
		{ID: "items", Method: "GET", Path: "/demo/items"},
		{ID: "refresh", Method: "POST", Path: "/demo/refresh"},
	}
	tools := []framework.ToolDigest{{ID: "ping", SHA256: framework.ModuleToolDigest(demoPingTool())}}
	d := framework.ProcessModuleDescriptor{
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
	surface, err := framework.ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatalf("compute surface sha: %v", err)
	}
	d.SurfaceSHA256 = surface
	return d
}

// demoRunner spawns the demo binary under framework.TrustedProcessRunner, injecting the
// given DEMO_* env knobs via framework.ChildSpec.ExtraEnv. It is the gate-test analog of
// supervisor_test.go's envRunner, but the artifact is the real demo binary
// (not a re-exec of the test binary).
type demoRunner struct {
	env map[string]string
}

func (r *demoRunner) Start(ctx context.Context, spec framework.ChildSpec) (framework.RunningChild, error) {
	for k, v := range r.env {
		spec.ExtraEnv = append(spec.ExtraEnv, k+"="+v)
	}
	return (&framework.TrustedProcessRunner{}).Start(ctx, spec)
}

// newGateSupervisor builds a test-friendly supervisor with the given store,
// broker, runner, and replica id. Knobs mirror newTestSupervisor so the
// poll/lease/backoff/circuit timings stay compressed.
func newGateSupervisor(t testing.TB, store framework.ProcessModuleStore, broker framework.ReverseBroker, runner framework.Runner, replicaID string) *framework.ProcessModuleSupervisor {
	t.Helper()
	sup, err := framework.NewProcessModuleSupervisor(framework.SupervisorConfig{
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
func registerEnableReady(t testing.TB, sup *framework.ProcessModuleSupervisor, d framework.ProcessModuleDescriptor, approved framework.ApprovedGrants) {
	t.Helper()
	if _, err := sup.Register(context.Background(), d, approved); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForStateTB(t, sup, d.Name, framework.StateReady, 6*time.Second)
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
func waitForStateTB(tb testing.TB, sup *framework.ProcessModuleSupervisor, name string, want framework.ProcessState, timeout time.Duration) {
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
func proxyGet(t testing.TB, sup *framework.ProcessModuleSupervisor, name, routeID, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/"+routeID, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	sup.ProxyHandler(name, routeID).ServeHTTP(rec, req)
	return rec
}

// =====================================================================
// Item 1: Crash containment (design §10.1)
// =====================================================================

// TestGate_CrashContainment proves the headline property through the real
// child: a module that os.Exit(1)s MID module.http handler cannot crash the
// host, and the in-flight response is a BUFFERED 503 (never a truncated 200).
// The supervisor restarts the child and the circuit charges the crash. The
// "disabled → 404" and "enabled-but-not-Ready → 503 + Retry-After" halves of
// the two-layer gate are pinned by TestGate_NotReadyAndDisabledGates below.
func TestGate_CrashContainment(t *testing.T) {
	store := newTestStore(t)
	sup := newGateSupervisor(t, store, framework.NopBroker{}, &demoRunner{env: map[string]string{
		"DEMO_CRASH_ON": "hello",
	}}, "replica-1")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, sup, d, framework.ApprovedGrants{"articles:read"})

	// Hit /hello. The child exits mid-handler (DEMO_CRASH_ON=hello). The
	// host's in-flight Call observes the dead child; the fully-buffered
	// response path surfaces a 503, never a partial 200.
	rec := proxyGet(t, sup, d.Name, "hello", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("mid-crash /hello: status = %d, want 503 (buffered)", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("mid-crash /hello: Retry-After header missing")
	}
	if rec.Body.String() == "" || strings.Contains(rec.Body.String(), "Demo module") {
		t.Errorf("mid-crash /hello: body must be the 503 sentinel, not child content: %q", rec.Body.String())
	}

	// The host stayed up: the supervisor must RESTART the child and charge
	// the crash to the circuit. The exit is asynchronous (the child dies on
	// its own ~20ms into the handler), so we wait for the restart to land:
	// RestartCount > 0 AND state back to Ready, rather than racing the
	// exit-watcher.
	deadline := time.Now().Add(8 * time.Second)
	restarted := false
	for time.Now().Before(deadline) {
		info, _ := sup.Info(d.Name)
		if info.RestartCount > 0 && info.State == framework.StateReady {
			restarted = true
			if info.CircuitOpen {
				t.Errorf("circuit should not be open after a single crash (threshold=5)")
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !restarted {
		info, _ := sup.Info(d.Name)
		t.Fatalf("child did not restart after self-crash: state=%s restarts=%d", info.State, info.RestartCount)
	}
	// A second route is unaffected, the host is healthy.
	rec2 := proxyGet(t, sup, d.Name, "tree", "")
	if rec2.Code != http.StatusOK {
		t.Errorf("post-restart /tree: status = %d, want 200 (host healthy)", rec2.Code)
	}
}

// TestGate_NotReadyAndDisabledGates pins the rest of item 1 + decision D:
// a freshly-enabled module is 503+Retry-After during the Starting window
// (exercised via DEMO_READY_DELAY_MS so the window is reliably observable),
// then serves 200 once Ready; a disabled module's proxy is not-Ready → 503
// (the route gate's 404 is unit-proven by the module route gate and asserted
// here at the state level: a disabled module sits at InstalledDisabled).
func TestGate_NotReadyAndDisabledGates(t *testing.T) {
	store := newTestStore(t)
	sup := newGateSupervisor(t, store, framework.NopBroker{}, &demoRunner{env: map[string]string{
		"DEMO_READY_DELAY_MS": "400",
	}}, "replica-1")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()

	// Disabled (default): state is InstalledDisabled. The route gate 404s
	// (module.go routeGate); serveProxy is reached only when the gate is
	// open, and a not-Ready module returns 503 there. Here we assert the
	// state that drives the gate.
	if info, _ := sup.Info(d.Name); info.State != framework.StateInstalledDisabled {
		t.Fatalf("disabled state = %s, want InstalledDisabled", info.State)
	}

	// Enable. With a 400ms ready delay, the enabled-but-not-Ready window
	// is wide enough to observe a 503+Retry-Before before Ready lands.
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	saw503 := false
	for range 40 {
		if rec := proxyGet(t, sup, d.Name, "tree", ""); rec.Code == http.StatusServiceUnavailable {
			if rec.Header().Get("Retry-After") == "" {
				t.Error("enabled-but-not-Ready 503 missing Retry-After")
			}
			saw503 = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !saw503 {
		t.Fatal("never observed the enabled-but-not-Ready 503 window")
	}
	// Once Ready, the same route serves 200.
	waitForState(t, sup, d.Name, framework.StateReady, 6*time.Second)
	if rec := proxyGet(t, sup, d.Name, "tree", ""); rec.Code != http.StatusOK {
		t.Errorf("ready /tree: status = %d, want 200", rec.Code)
	}
}

// =====================================================================
// Item 3: Sandbox conformance (design §10.3)
// =====================================================================

// TestGate_SandboxConformanceSelection proves the fail-closed SELECTION
// wiring end to end: a framework.TrustUntrusted descriptor with no probe-passing
// SandboxRunner is refused at Register (never reaches Ready; no silent
// downgrade to framework.TrustedProcessRunner). The P1–P7 probe internals are unit-
// proved by processmodule_probe_test.go; this asserts the selection gate
// fires against the real demo descriptor.
func TestGate_SandboxConformanceSelection(t *testing.T) {
	store := newTestStore(t)
	// Supervisor with NO sandbox configured (nil SandboxRunner).
	sup := newGateSupervisor(t, store, framework.NopBroker{}, &demoRunner{}, "replica-1")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustUntrusted)

	_, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"})
	var untrusted *framework.UntrustedNoSandboxError
	if !errors.As(err, &untrusted) {
		t.Fatalf("Register untrusted w/o sandbox: want framework.UntrustedNoSandboxError, got %v", err)
	}
	if !errors.Is(err, framework.ErrSandboxUnavailable) {
		t.Errorf("err must wrap framework.ErrSandboxUnavailable, got %v", err)
	}
	// The module never reached the slots map (no chance to spawn).
	if sup.Slot(d.Name) != nil {
		t.Error("untrusted module with no sandbox must not be registered into a slot")
	}
}

// =====================================================================
// Item 4: DDL isolation (design §10.4)
// =====================================================================

// TestGate_DDLIsolation_ReadyGate asserts the Ready-gate holds a module with
// a declared migration group while MigrationsAppliedAt is nil (proxy 503,
// never spawns), then releases it once the coordinator stamps the timestamp.
// The actual Postgres isolation (module_<M> schema+role, public denied,
// SET ROLE refused) is proved by TestCoord_PG_* in
// processmodule_migrate_test.go, this test does NOT duplicate the
// testcontainer; it pins the supervisor-side Ready-gate that gating depends on.
func TestGate_DDLIsolation_ReadyGate(t *testing.T) {
	store := newTestStore(t)
	sup := newGateSupervisor(t, store, framework.NopBroker{}, &demoRunner{}, "replica-1")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "demo", framework.TrustTrusted)
	if _, err := sup.Register(context.Background(), d, framework.ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Migrations pending: the module is held at InstalledDisabled and the
	// proxy returns 503. It must NOT spawn (state stays InstalledDisabled).
	time.Sleep(150 * time.Millisecond) // let a couple of poll ticks fire
	if info, _ := sup.Info(d.Name); info.State != framework.StateInstalledDisabled {
		t.Fatalf("pending-migrations state = %s, want InstalledDisabled (no spawn)", info.State)
	}
	if rec := proxyGet(t, sup, d.Name, "tree", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("pending-migrations /tree: status = %d, want 503", rec.Code)
	}

	// The coordinator stamps MigrationsAppliedAt. Bump generation so every
	// replica's reconcile re-arms deterministically (the poll would also
	// catch it; the bump removes the race).
	now := time.Now()
	if err := store.SetMigrationsAppliedAt(context.Background(), d.Name, &now); err != nil {
		t.Fatalf("stamp migrations: %v", err)
	}
	if _, err := sup.BumpGeneration(context.Background(), d.Name); err != nil {
		t.Fatalf("bump generation: %v", err)
	}
	// Now it spawns to Ready and the proxy serves 200.
	waitForState(t, sup, d.Name, framework.StateReady, 6*time.Second)
	if rec := proxyGet(t, sup, d.Name, "tree", ""); rec.Code != http.StatusOK {
		t.Errorf("post-migration /tree: status = %d, want 200", rec.Code)
	}
}

// =====================================================================
// Item 5: UI containment (design §10.5 / §9)
// =====================================================================

// TestGate_UIContainment proves the closed ui.node.v1 validator is the render
// path's gate, end to end through the real child.
//
// decodeBody (processmodule_proxy.go) validates a ui.node.v1 body through
// uinodev1.Validate and renders it host-side as text/html, with the host
// assigning every id, class, ARIA attribute and data-fui-rpc. A validation or
// render failure is fail-safe: it surfaces as a buffered 503 and the module's
// content never reaches the wire.
//
//   - DEMO_FORGE_DATAFUI: the child returns a tree smuggling a data-fui-rpc
//     prop. Validate whole-tree rejects it, so /hello is a 503 and the forged
//     attribute never reaches the response body. The /tree route returns the
//     SAME bytes as a json body, which the proxy passes through, showing the
//     bytes really do carry the forgery and that the validator is what stops
//     it rather than the transport.
//   - Clean tree: Validate passes and the wired renderer serves it, so /hello
//     answers 200 with host-assigned markup.
func TestGate_UIContainment(t *testing.T) {
	// --- forged tree ---
	store := newTestStore(t)
	forgeSup := newGateSupervisor(t, store, framework.NopBroker{}, &demoRunner{env: map[string]string{
		"DEMO_FORGE_DATAFUI": "1",
	}}, "replica-1")
	dForged := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, forgeSup, dForged, framework.ApprovedGrants{"articles:read"})

	// /hello (ui.node.v1): the render path validates the tree before rendering,
	// so a forged data-fui-* prop whole-tree rejects → served-safe 503; the
	// forged attribute must NEVER appear in the response body.
	helloRec := proxyGet(t, forgeSup, dForged.Name, "hello", "")
	if helloRec.Code != http.StatusServiceUnavailable {
		t.Errorf("forged /hello: status = %d, want 503 (validator rejected, served-safe)", helloRec.Code)
	}
	if strings.Contains(helloRec.Body.String(), "data-fui-rpc") {
		t.Errorf("forged /hello: data-fui-rpc leaked into the response body: %q", helloRec.Body.String())
	}

	// /tree returns the forged bytes as json (passes through). The closed
	// validator whole-tree rejects them.
	treeRec := proxyGet(t, forgeSup, dForged.Name, "tree", "")
	if treeRec.Code != http.StatusOK {
		t.Fatalf("forged /tree: status = %d, want 200 (json passes through)", treeRec.Code)
	}
	if _, err := uinodev1.Validate(treeRec.Body.Bytes(), uinodev1.Limits{}); err == nil {
		t.Error("forged tree passed uinodev1.Validate; want whole-tree reject")
	}

	// --- clean tree ---
	cleanSup := newGateSupervisor(t, newTestStore(t), framework.NopBroker{}, &demoRunner{}, "replica-2")
	dClean := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, cleanSup, dClean, framework.ApprovedGrants{"articles:read"})

	cleanTreeRec := proxyGet(t, cleanSup, dClean.Name, "tree", "")
	if cleanTreeRec.Code != http.StatusOK {
		t.Fatalf("clean /tree: status = %d, want 200", cleanTreeRec.Code)
	}
	tree, err := uinodev1.Validate(cleanTreeRec.Body.Bytes(), uinodev1.Limits{})
	if err != nil {
		t.Errorf("clean tree rejected by uinodev1.Validate: %v", err)
	}
	if tree != nil && string(tree.Root.Component) != "card" {
		t.Errorf("clean tree root = %q, want card", tree.Root.Component)
	}
	// /hello on the clean tree now renders through the wired render path:
	// 200, text/html, real design-system markup, and NOTHING the module
	// supplied (no id/class/data-* from the tree; the host assigns all of it).
	helloClean := proxyGet(t, cleanSup, dClean.Name, "hello", "")
	if helloClean.Code != http.StatusOK {
		t.Fatalf("clean /hello: status = %d, want 200 (rendered)", helloClean.Code)
	}
	if ct := helloClean.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("clean /hello: content-type = %q, want text/html", ct)
	}
	rendered := helloClean.Body.String()
	if !strings.Contains(rendered, "<") || strings.TrimSpace(rendered) == "" {
		t.Errorf("clean /hello: expected rendered markup, got %q", rendered)
	}
	// The demo's action_ref resolves to a declared route; a dead ref would
	// have failed the render closed (503). Reaching 200 proves resolution.
}

// =====================================================================
// Capability-broker environment (shared by items 2 + 6)
// =====================================================================

// gateCapEnv is the real CRUD + capability-broker environment for the
// capability gate (item 2) and the convergence/revoke gate (item 6). It wires
// two owner-scoped entities (articles, secrets) over one SQLite DB, registers
// CRUD routes behind an auth middleware that resolves Cookie "sid=<user>" →
// user (so RequireOwner filters rows to the caller's owner id), and exposes a
// fresh Broker factory so each supervisor gets its own broker over the shared
// data. It uses local copies of the broker-test helpers from the framework
// root's processmodule_broker_test.go (brokerEntity / brokerAuthMiddleware
// / …) so the wiring stays identical to that suite.
type gateCapEnv struct {
	db       *sql.DB
	router   http.Handler
	registry *framework.Registry
	policy   *access.RolePolicy
}

// newGateCapEnv builds the env and applies the seed DDL. Rows are seeded per
// test (different gate items want different owner distributions).
func newGateCapEnv(t *testing.T) *gateCapEnv {
	t.Helper()
	// Register the global owner extractor so RequireOwner (owner.Get) can
	// resolve the Cookie-derived brokerTestUser to an owner id, the same
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
func (e *gateCapEnv) newBroker() *framework.Broker {
	return framework.NewBroker(e.router, e.registry, nil, "", framework.WithBrokerPolicy(e.policy))
}

// =====================================================================
// Item 2: Capability containment (design §10.2)
// =====================================================================

// TestGate_CapabilityContainment proves, through the REAL child's reverse
// host.entity.query, that:
//   - a GRANTED entity returns the caller's OWNER-SCOPED rows only (the
//     delegated caller's context is re-attached by the broker, and CRUD
//     RequireOwner filters to the caller's owner id);
//   - an entity OUTSIDE the module's grant set is denied at the broker's
//     module-grant gate (the router is never reached);
//   - the confused-deputy control holds: EntityQueryParams has NO capability
//     field, so the required permission (articles:read / secrets:read) is
//     derived from the trusted method + the entity NAME in the call, the
//     child cannot name its own permission label or talk its way into
//     secrets:read.
//
// The finer-grained denies (CrossOwnerRead carve-out, caller-authority 403,
// ambient owner-scope refusal) are unit-proven by processmodule_broker_test;
// this is the end-to-end-through-the-real-child path.
func TestGate_CapabilityContainment(t *testing.T) {
	env := newGateCapEnv(t)
	brokerSeedRow(t, env.db, "articles", "a1", "userA", "Alpha-one")
	brokerSeedRow(t, env.db, "articles", "a2", "userA", "Alpha-two")
	brokerSeedRow(t, env.db, "articles", "a3", "userB", "Beta-one")
	brokerSeedRow(t, env.db, "secrets", "s1", "userA", "Secret-alpha")

	// --- positive: granted entity, owner-scoped rows ---
	supA := newGateSupervisor(t, newTestStore(t), env.newBroker(), &demoRunner{env: map[string]string{
		"DEMO_QUERY_ENTITY": "articles",
	}}, "replica-A")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, supA, d, framework.ApprovedGrants{"articles:read"})

	rec := proxyGet(t, supA, d.Name, "items", "sid=userA")
	if rec.Code != http.StatusOK {
		t.Fatalf("granted /items as userA: status = %d, want 200", rec.Code)
	}
	var body struct {
		Entity string          `json:"entity"`
		Total  int             `json:"total"`
		Rows   json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /items body: %v: %s", err, rec.Body.String())
	}
	if body.Total != 2 {
		t.Errorf("granted /items total = %d, want 2 (userA's articles only, owner scope held)", body.Total)
	}
	if !strings.Contains(string(body.Rows), "Alpha-one") {
		t.Errorf("granted /items rows missing userA's Alpha-one: %s", body.Rows)
	}
	if strings.Contains(string(body.Rows), "Beta-one") {
		t.Errorf("granted /items rows leaked userB's row across owners: %s", body.Rows)
	}

	// --- denial: entity outside the grant set (confused-deputy) ---
	// The child is granted ONLY articles:read but reverse-queries secrets.
	// The broker derives secrets:read from the trusted method + entity name
	// and denies at the module-grant gate; the child surfaces a 403.
	supB := newGateSupervisor(t, newTestStore(t), env.newBroker(), &demoRunner{env: map[string]string{
		"DEMO_QUERY_ENTITY": "secrets",
	}}, "replica-B")
	dB := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, supB, dB, framework.ApprovedGrants{"articles:read"})

	recB := proxyGet(t, supB, dB.Name, "items", "sid=userA")
	if recB.Code != http.StatusForbidden {
		t.Errorf("outside-grant /items (secrets, granted only articles:read): status = %d, want 403 (denied)", recB.Code)
	}
	if strings.Contains(recB.Body.String(), "Secret-alpha") {
		t.Errorf("denied reverse call leaked secret data: %s", recB.Body.String())
	}
}

// =====================================================================
// Item 6: Convergence + revoke (design §10.6)
// =====================================================================

// TestGate_ConvergenceAndRevoke (the cross-replica convergence half of this
// item) stayed in the framework root test package: it hand-constructs a
// moduleSlot for the second replica, which is unexported supervisor state,
// and exporting a slot-construction seam would be API that exists only for
// this test.

// TestGate_RevokeDeniesReverseCall is the enforcement half of item 6: after
// RevokeGrants bumps the generation and the replica respawns the child at the
// new generation, the revoked capability's next reverse call MUST be denied:
// no token-expiry window. This exercises the fail-closed respawn path:
// finishDrain re-reads the store's authoritative EffectiveGrants (now empty)
// so the new child's broker view no longer carries articles:read.
func TestGate_RevokeDeniesReverseCall(t *testing.T) {
	env := newGateCapEnv(t)
	brokerSeedRow(t, env.db, "articles", "a1", "userA", "Alpha-one")

	store := newTestStore(t)
	sup := newGateSupervisor(t, store, env.newBroker(), &demoRunner{env: map[string]string{
		"DEMO_QUERY_ENTITY": "articles",
	}}, "replica-1")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, sup, d, framework.ApprovedGrants{"articles:read"})

	// Pre-revoke: /items (a reverse host.entity.query for articles) succeeds.
	if pre := proxyGet(t, sup, d.Name, "items", "sid=userA"); pre.Code != http.StatusOK {
		t.Fatalf("pre-revoke /items: status = %d, want 200", pre.Code)
	}

	// Revoke articles:read → generation bumps, the child drains and respawns
	// from the store's now-empty EffectiveGrants.
	if _, err := sup.RevokeGrants(context.Background(), d.Name, nil); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	waitForState(t, sup, d.Name, framework.StateReady, 6*time.Second)

	// Post-revoke: the same reverse query is now denied at the broker's
	// module-grant gate (the child no longer holds articles:read).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := proxyGet(t, sup, d.Name, "items", "sid=userA")
		switch got.Code {
		case http.StatusForbidden:
			return // revoke enforced end-to-end
		case http.StatusOK, http.StatusServiceUnavailable:
			// Transient: 200 = old grant view still live mid-respawn;
			// 503 = enabled-but-not-Ready during the upgrade drain. Retry
			// until the new (empty) grant view is serving, then it must 403.
			time.Sleep(50 * time.Millisecond)
			continue
		default:
			t.Fatalf("post-revoke /items: status = %d, want 403 (or transient 200/503)", got.Code)
		}
	}
	t.Fatal("post-revoke /items never returned 403, revoked grant still served after respawn")
}

// TestGate_GenerationBumpResetsCircuit proves the other half of item 6: a
// generation bump is the operator's single recovery lever and RESETS a
// tripped restart circuit on every replica. The circuit-open + reset
// mechanics are unit-proven by TestSupervisor_CircuitOpensAndGenResets; this
// pins them through the real demo child (crashed via DEMO_CRASH_ON).
func TestGate_GenerationBumpResetsCircuit(t *testing.T) {
	sup := newGateSupervisor(t, newTestStore(t), framework.NopBroker{}, &demoRunner{env: map[string]string{
		"DEMO_CRASH_ON": "tree",
	}}, "replica-1")
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", framework.TrustTrusted)
	registerEnableReady(t, sup, d, framework.ApprovedGrants{"articles:read"})

	// Crash the child repeatedly (DEMO_CRASH_ON=tree) until the circuit
	// opens (5 crashes within the 10s window). Once the circuit trips the
	// child can NEVER return to Ready, so the loop must poll for
	// Ready-OR-circuit-open rather than block on Ready (a plain waitForState
	// would fatal-timeout the instant the circuit opens mid-loop).
	circuitOpen := false
	overall := time.Now().Add(30 * time.Second)
	for time.Now().Before(overall) {
		// Wait for either Ready (child restarted from the previous crash)
		// or the circuit to open, whichever happens first.
		ready := false
		step := time.Now().Add(5 * time.Second)
		for time.Now().Before(step) {
			si, _ := sup.Info(d.Name)
			if si.CircuitOpen {
				circuitOpen = true
				break
			}
			if si.State == framework.StateReady {
				ready = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if circuitOpen {
			break
		}
		if !ready {
			continue // still mid-restart; retry the wait
		}
		_ = proxyGet(t, sup, d.Name, "tree", "") // triggers a crash
		time.Sleep(60 * time.Millisecond)        // let reconcile charge it
		si, _ := sup.Info(d.Name)
		if si.CircuitOpen {
			circuitOpen = true
			break
		}
	}
	if !circuitOpen {
		info, _ := sup.Info(d.Name)
		t.Fatalf("circuit did not open after repeated crashes (last restarts=%d)", info.RestartCount)
	}

	// Generation bump → circuit resets → child restarts to Ready.
	if _, err := sup.BumpGeneration(context.Background(), d.Name); err != nil {
		t.Fatalf("bump generation: %v", err)
	}
	waitForState(t, sup, d.Name, framework.StateReady, 6*time.Second)
	after, _ := sup.Info(d.Name)
	if after.CircuitOpen {
		t.Errorf("circuit still open after generation bump (want reset)")
	}
}
