package framework

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// Tests for the App-level untrusted-tier wiring (issue #608):
// WithSandboxBackend, App.ProcessModuleSandbox, and RegisterProcessModule
// handing the supervisor a probed SandboxRunner. The real host backends
// are exercised by TestSandboxConformance and by the processmodule-demo
// host example's tests; here a scripted backend stands in so the wiring
// is tested on every host.

// scriptedBackend answers every probe with a fixed result line. Its Wrap
// replaces the probe child with `/bin/sh -c 'echo <line>'`, so the
// conformance report comes out exactly as scripted without confining
// anything. Only for testing the wiring around the probe.
type scriptedBackend struct {
	line    string
	wrapped int
}

func (b *scriptedBackend) Name() string              { return "scripted" }
func (b *scriptedBackend) Available() bool           { return true }
func (b *scriptedBackend) MissingReason() string     { return "" }
func (b *scriptedBackend) DeclaredProbes() []ProbeID { return allProbes }
func (b *scriptedBackend) Wrap(cmd *exec.Cmd, _ SandboxOpts) error {
	b.wrapped++
	cmd.Path = "/bin/sh"
	cmd.Args = []string{"/bin/sh", "-c", "echo " + b.line}
	return nil
}

func skipWithoutSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("scripted backend shells to /bin/sh")
	}
}

func untrustedDescriptor() ProcessModuleDescriptor {
	d := validDescriptor()
	d.TrustTier = TrustUntrusted
	return d
}

// recoverPanic runs fn and returns its panic value rendered as a string,
// or "" when fn returned normally.
func recoverPanic(fn func()) (msg string) {
	defer func() {
		if p := recover(); p != nil {
			msg = fmt.Sprint(p)
		}
	}()
	fn()
	return ""
}

// An untrusted descriptor registers on an app whose backend conforms, and
// its slot runs under the app's SandboxRunner, never the trusted runner.
func TestApp_UntrustedModuleRegistersWithConformingBackend(t *testing.T) {
	skipWithoutSh(t)
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	backend := &scriptedBackend{line: probeOutPass}
	app := NewApp(WithDB(db), WithSandboxBackend(backend))

	desc := untrustedDescriptor()
	app.RegisterProcessModule(desc, ApprovedGrants{"articles:read"})

	sb, err := app.ProcessModuleSandbox()
	if err != nil {
		t.Fatalf("ProcessModuleSandbox: %v", err)
	}
	if !sb.Conforms() || sb.Backend() != backend {
		t.Fatalf("sandbox runner = %+v, want the conforming scripted backend", sb.Report())
	}
	slot := app.ProcessModules().Slot(desc.Name)
	if slot == nil {
		t.Fatalf("untrusted module %q not registered", desc.Name)
	}
	if slot.runner != Runner(sb) {
		t.Fatalf("untrusted slot runner = %T, want the app's *SandboxRunner", slot.runner)
	}
}

// The probe runs once per App: a second untrusted module and a direct
// ProcessModuleSandbox call reuse the kept result.
func TestApp_ProcessModuleSandboxProbesOnce(t *testing.T) {
	skipWithoutSh(t)
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	backend := &scriptedBackend{line: probeOutPass}
	app := NewApp(WithDB(db), WithSandboxBackend(backend))

	app.RegisterProcessModule(untrustedDescriptor(), ApprovedGrants{"articles:read"})
	probes := backend.wrapped
	if probes != len(allProbes) {
		t.Fatalf("first registration wrapped %d probe children, want %d", probes, len(allProbes))
	}
	second := untrustedDescriptor()
	second.Name = "demo2"
	second.Routes = []RouteDeclaration{{ID: "list", Method: "GET", Path: "/demo2/items"}}
	app.RegisterProcessModule(second, ApprovedGrants{"articles:read"})
	if _, err := app.ProcessModuleSandbox(); err != nil {
		t.Fatalf("ProcessModuleSandbox: %v", err)
	}
	if backend.wrapped != probes {
		t.Fatalf("probe ran again: %d wraps after the first registration, want %d", backend.wrapped, probes)
	}
}

// A backend that does not conform refuses the registration with the
// conformance report in the message, and nothing is registered.
func TestApp_UntrustedModuleNonConformingPanicsWithReport(t *testing.T) {
	skipWithoutSh(t)
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithSandboxBackend(&scriptedBackend{line: probeOutBreach + " scripted-breach"}))

	desc := untrustedDescriptor()
	msg := recoverPanic(func() { app.RegisterProcessModule(desc, ApprovedGrants{"articles:read"}) })
	for _, want := range []string{"TrustUntrusted", "does not conform", "scripted-breach", "NOT CONFORMING"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic = %q, want it to contain %q", msg, want)
		}
	}
	if sl := app.ProcessModules().Slot(desc.Name); sl != nil {
		t.Fatal("a refused untrusted module still got a supervisor slot")
	}
	_, err := app.ProcessModuleSandbox()
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("ProcessModuleSandbox err = %v, want ErrSandboxUnavailable", err)
	}
}

// WithSandboxBackend(nil) is the explicit "no untrusted modules here"
// choice: it never falls back to the host default.
func TestApp_UntrustedModuleNilBackendFailsClosed(t *testing.T) {
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithSandboxBackend(nil))

	msg := recoverPanic(func() { app.RegisterProcessModule(untrustedDescriptor(), ApprovedGrants{"articles:read"}) })
	if !strings.Contains(msg, "nil backend") {
		t.Fatalf("panic = %q, want the nil-backend refusal", msg)
	}
}

// An invalid untrusted descriptor is refused for what is wrong with it,
// before any probe runs.
func TestApp_UntrustedInvalidDescriptorRefusedBeforeProbe(t *testing.T) {
	skipWithoutSh(t)
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	backend := &scriptedBackend{line: probeOutPass}
	app := NewApp(WithDB(db), WithSandboxBackend(backend))

	bad := untrustedDescriptor()
	bad.RequestedGrants = []access.Permission{"CrossOwnerRead"}
	msg := recoverPanic(func() { app.RegisterProcessModule(bad, ApprovedGrants{"CrossOwnerRead"}) })
	if msg == "" || strings.Contains(msg, "sandbox") {
		t.Fatalf("panic = %q, want the descriptor validation error", msg)
	}
	if backend.wrapped != 0 {
		t.Fatalf("the probe ran %d children for a descriptor that was refused anyway", backend.wrapped)
	}
}

// A trusted module never probes: an app that hosts no untrusted module
// pays nothing for the sandbox wiring.
func TestApp_TrustedModuleDoesNotProbe(t *testing.T) {
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	backend := &scriptedBackend{line: probeOutPass}
	app := NewApp(WithDB(db), WithSandboxBackend(backend))

	app.RegisterProcessModule(validDescriptor(), ApprovedGrants{"articles:read"})
	if backend.wrapped != 0 {
		t.Fatalf("a trusted registration ran %d probe children", backend.wrapped)
	}
}

// passthroughBackend wraps nothing: the probe child runs unconfined. Used
// only to see that the child runs the probe body at all.
type passthroughBackend struct{}

func (passthroughBackend) Name() string                      { return "passthrough" }
func (passthroughBackend) Available() bool                   { return true }
func (passthroughBackend) MissingReason() string             { return "" }
func (passthroughBackend) DeclaredProbes() []ProbeID         { return nil }
func (passthroughBackend) Wrap(*exec.Cmd, SandboxOpts) error { return nil }

// The probe child is answered by the package init, not by TestMain: the
// binary re-executed with GOFASTR_SANDBOX_PROBE set prints a result line
// on every OS. This is the dispatch a host app binary relies on, since it
// has no TestMain and no hook in its main.
func TestProbeChildDispatchedByPackageInit(t *testing.T) {
	res := runOneProbe(context.Background(), passthroughBackend{}, ProbeNoInheritedSecret, t.TempDir())
	if !res.Reported {
		t.Fatalf("probe child did not report (status=%s detail=%q): the binary ran something other than the probe body", res.Status, res.Detail)
	}
}

// probeScriptedBackend answers the conformance probe with PASS and runs
// every other command (the module child) unchanged. It lets the test
// follow an untrusted module through the app-wired SandboxRunner on any
// host; it confines nothing.
type probeScriptedBackend struct {
	probes, spawns int
}

func (b *probeScriptedBackend) Name() string              { return "probe-scripted" }
func (b *probeScriptedBackend) Available() bool           { return true }
func (b *probeScriptedBackend) MissingReason() string     { return "" }
func (b *probeScriptedBackend) DeclaredProbes() []ProbeID { return allProbes }
func (b *probeScriptedBackend) Wrap(cmd *exec.Cmd, _ SandboxOpts) error {
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, probeEnvName+"=") {
			b.probes++
			cmd.Path = "/bin/sh"
			cmd.Args = []string{"/bin/sh", "-c", "echo " + probeOutPass}
			return nil
		}
	}
	b.spawns++
	return nil
}

// An untrusted module registered through the App spawns through the
// app's SandboxRunner (its backend wraps the module child) and serves its
// routes once Ready: the production path from RegisterProcessModule to a
// confined child, end to end.
func TestApp_UntrustedModuleServesThroughAppSandbox(t *testing.T) {
	skipWithoutSh(t)
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	backend := &probeScriptedBackend{}
	app := NewApp(WithDB(db), WithSandboxBackend(backend))

	desc := demoDescriptor(t, nil, "", TrustUntrusted)
	app.RegisterProcessModule(desc, ApprovedGrants{})
	sup := app.ProcessModules()
	t.Cleanup(func() { _ = sup.Close(context.Background()) })
	sup.StartLoops() // what App.Start does
	if err := sup.Enable(context.Background(), desc.Name); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	waitForState(t, sup, desc.Name, StateReady, 20*time.Second)

	if backend.probes != len(allProbes) || backend.spawns < 1 {
		t.Fatalf("backend wrapped %d probes and %d module spawns, want %d probes and the module", backend.probes, backend.spawns, len(allProbes))
	}
	if info, _ := sup.Info(desc.Name); info.TrustTier != TrustUntrusted {
		t.Fatalf("module runs at tier %s, want untrusted", info.TrustTier)
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demo/hello", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello from the demo module") {
		t.Fatalf("GET /demo/hello = %d %q, want the demo screen", rec.Code, rec.Body.String())
	}
}

// The refusal carries the report as data, not only as text, for an
// operator surface that renders it.
func TestSandboxConformanceErrorCarriesReport(t *testing.T) {
	skipWithoutSh(t)
	_, err := NewSandboxRunner(&scriptedBackend{line: probeOutBreach + " scripted"}, SandboxOpts{})
	var cerr *SandboxConformanceError
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %T %v, want *SandboxConformanceError", err, err)
	}
	if cerr.Report.Backend != "scripted" || len(cerr.Report.Results) != len(allProbes) || cerr.Report.Conforms() {
		t.Fatalf("report = %+v, want seven non-conforming scripted results", cerr.Report)
	}
	for _, res := range cerr.Report.Results {
		if !res.Reported || res.Status != ProbeStatusFail {
			t.Fatalf("probe %s = %+v, want a reported breach", res.ID, res)
		}
	}
	_, err = NewSandboxRunner(&fakeBackend{name: "gone", missing: "not installed"}, SandboxOpts{})
	if !errors.As(err, &cerr) || cerr.Report.Available || cerr.Report.MissingReason != "not installed" {
		t.Fatalf("unavailable backend: err = %v, want a report naming the missing reason", err)
	}
}
