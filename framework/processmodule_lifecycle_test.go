package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// TestEnableAfterStartLoopsSpawns: a module registered after StartLoops
// gets a supervise loop from Enable. The old once-guard waited for
// StateAbsent, which Register never sets, so the module sat in
// InstalledDisabled forever.
func TestEnableAfterStartLoopsSpawns(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	store := newTestStore(t)
	d := descriptorForChild(t, childModeEcho)
	sup := newTestSupervisor(t, store, childModeEcho)
	base := superviseLoops()
	sup.StartLoops()
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 8*time.Second)

	// Repeat toggles and a second StartLoops must not stack loops: one
	// slot, one supervise goroutine.
	for range 2 {
		if err := sup.Enable(context.Background(), d.Name); err != nil {
			t.Fatalf("re-enable: %v", err)
		}
	}
	sup.StartLoops()
	if n := superviseLoops() - base; n > 1 {
		t.Errorf("one slot runs %d supervise loops, want 1", n)
	}
}

// TestLeaseDrainChargesNoCrash: the lease-expired drain kills a healthy
// child on purpose. Its exit is not a crash, so store outages must not
// charge the restart circuit; enough of them would wedge the module in
// Crashed with the circuit open.
func TestLeaseDrainChargesNoCrash(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	store := &redLeaseFlakyStore{SQLProcessModuleStore: newTestStore(t)}
	d := descriptorForChild(t, childModeEcho)
	sup := newTestSupervisor(t, store, childModeEcho)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 8*time.Second)

	// The exit watcher's rule on its own, free of the race with
	// handleLeaseExpired's state restore: a child that exits while the
	// lease is failing was drained, not crashed. The store stays broken
	// for well under LeaseTTL, so reconcile neither clears the flag nor
	// runs the drain itself.
	store.broken.Store(true)
	sl := sup.Slot(d.Name)
	sl.mu.Lock()
	sl.leaseFailing = true
	child := sl.child
	sl.mu.Unlock()
	_ = child.Kill()
	deadline := time.Now().Add(300 * time.Millisecond)
	for redLeaseSlotPID(sup, d.Name) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if info, _ := sup.Info(d.Name); info.State == StateCrashed {
		t.Fatalf("a child killed during a lease drain was recorded as a crash (last exit %q)", info.LastExit)
	}
	store.broken.Store(false)
	waitForState(t, sup, d.Name, StateReady, 8*time.Second)

	for outage := 1; outage <= 3; outage++ {
		before, _ := sup.Info(d.Name)
		store.broken.Store(true)
		deadline := time.Now().Add(3 * time.Second)
		for redLeaseSlotPID(sup, d.Name) > 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if pid := redLeaseSlotPID(sup, d.Name); pid > 0 {
			t.Fatalf("outage %d: lease never drained the child (pid %d)", outage, pid)
		}
		// Let the exit watcher run before the store returns.
		time.Sleep(100 * time.Millisecond)
		store.broken.Store(false)

		deadline = time.Now().Add(8 * time.Second)
		var info ProcessModuleInfo
		for time.Now().Before(deadline) {
			info, _ = sup.Info(d.Name)
			if info.State == StateReady && !info.LeaseFailing && info.InstanceID != before.InstanceID {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if info.RestartCount != 0 {
			t.Fatalf("outage %d: a lease drain charged the restart circuit (RestartCount=%d, state %s, last exit %q)",
				outage, info.RestartCount, info.State, info.LastExit)
		}
		if info.State != StateReady {
			t.Fatalf("outage %d: module did not recover to Ready, state %s", outage, info.State)
		}
	}
}

// TestProxyOversizeBody413: a body over the proxy's 1 MiB cap is refused
// with 413, never forwarded to the module truncated.
func TestProxyOversizeBody413(t *testing.T) {
	sup, sl, _, child, cleanup := toolTestSlot(t, nil)
	defer cleanup()
	got := make(chan int, 4)
	if err := child.Handle(moduleproto.MethodHTTP, func(_ context.Context, raw json.RawMessage) (any, error) {
		var p moduleproto.HTTPRequestParams
		_ = json.Unmarshal(raw, &p)
		got <- len(p.BodyB64)
		return moduleproto.HTTPResponseResult{
			Status: http.StatusOK,
			Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindText, Value: jsonRaw(`"ok"`)},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	small := httptest.NewRecorder()
	sup.serveProxy(sl.desc.Name, "r1", small, httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader([]byte("hi"))))
	if small.Code != http.StatusOK {
		t.Fatalf("small body: status %d, want 200 (%q)", small.Code, small.Body.String())
	}
	<-got

	big := bytes.Repeat([]byte("x"), maxModuleHTTPBodyBytes+1)
	rec := httptest.NewRecorder()
	sup.serveProxy(sl.desc.Name, "r1", rec, httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(big)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: status %d, want 413", rec.Code)
	}
	select {
	case n := <-got:
		t.Errorf("oversize body reached the module truncated (%d base64 bytes)", n)
	default:
	}

	// The cap itself is allowed; one byte over is not.
	atCap := httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(big[:maxModuleHTTPBodyBytes]))
	if _, err := buildHTTPRequestParams(atCap, "demo", "r1", "id", "h", time.Second); err != nil {
		t.Errorf("body of exactly the cap refused: %v", err)
	}
	over := httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(big))
	if _, err := buildHTTPRequestParams(over, "demo", "r1", "id", "h", time.Second); !errors.Is(err, errModuleBodyTooLarge) {
		t.Errorf("body one byte over the cap: err %v, want errModuleBodyTooLarge", err)
	}
}

// TestRunnerRemovesScratchDir: the runner-made scratch dir goes away
// when the child is reaped, so a crash-looping module does not leave one
// directory per restart under the temp root.
func TestRunnerRemovesScratchDir(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	d := descriptorForChild(t, childModeCrashExit)
	gone := func(what, instance string) {
		t.Helper()
		dir := filepath.Join(os.TempDir(), "gofastr-module-"+safeDirName(d.Name)+"-"+shortID(instance))
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			_ = os.RemoveAll(dir)
			t.Errorf("%s: scratch dir %s survived (stat err %v)", what, dir, err)
		}
	}

	instance := mintInstanceID()
	runner := &envRunner{inner: &TrustedProcessRunner{}, env: childEnv(childModeCrashExit, nil)}
	rc, err := runner.Start(context.Background(), ChildSpec{Descriptor: d, InstanceID: instance})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = rc.Wait()
	gone("child reaped", instance)

	// A spawn that fails after exec (here the codec) owns no child to
	// reap later, so the runner removes the dir itself.
	instance = mintInstanceID()
	failCodec := &TrustedProcessRunner{NewCodec: func(io.Reader, io.Writer, int) (*moduleproto.Codec, error) {
		return nil, errors.New("codec refused")
	}}
	if _, err := (&envRunner{inner: failCodec, env: childEnv(childModeCrashExit, nil)}).Start(context.Background(), ChildSpec{Descriptor: d, InstanceID: instance}); err == nil {
		t.Fatal("start with a failing codec: want error")
	}
	gone("codec failure", instance)

	// A sandbox that refuses to wrap never execs; the dir still goes.
	instance = mintInstanceID()
	sb := newFakeConformingSandbox(&fakeBackend{name: "fake", available: true, declared: allProbes, wrapErr: errors.New("wrap refused")})
	if _, err := sb.Start(context.Background(), ChildSpec{Descriptor: d, InstanceID: instance}); err == nil {
		t.Fatal("start with a refusing sandbox: want error")
	}
	gone("wrap failure", instance)

	// A caller-supplied dir is the caller's: it survives the child.
	own := t.TempDir()
	rc, err = runner.Start(context.Background(), ChildSpec{Descriptor: d, InstanceID: mintInstanceID(), ScratchDir: own})
	if err != nil {
		t.Fatalf("start with a caller dir: %v", err)
	}
	_ = rc.Wait()
	if _, err := os.Stat(own); err != nil {
		t.Errorf("the runner removed a caller-supplied scratch dir: %v", err)
	}
}

// TestProcessModuleNoSupervisorClosed: a process module whose manager has
// no supervisor to ask is disabled and cannot be enabled, never open.
func TestProcessModuleNoSupervisorClosed(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("p", ModuleManifest{})
	mm.markProcess("p")
	if err := mm.loadFromStore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mm.Enabled("p") {
		t.Error("process module with no supervisor reads enabled")
	}
	if err := mm.Enable(context.Background(), "p"); err == nil {
		t.Error("Enable of a process module with no supervisor succeeded")
	}
}

// TestEnableBeforeStartLoopsWaits: Enable before StartLoops records the
// desired state but spawns nothing; StartLoops then brings the module up.
func TestEnableBeforeStartLoopsWaits(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	d := descriptorForChild(t, childModeEcho)
	sup := newTestSupervisor(t, newTestStore(t), childModeEcho)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if info, _ := sup.Info(d.Name); info.State != StateInstalledDisabled {
		t.Fatalf("Enable before StartLoops moved the module to %s, want InstalledDisabled", info.State)
	}
	sup.StartLoops()
	waitForState(t, sup, d.Name, StateReady, 8*time.Second)
}

// superviseLoops counts the running supervise goroutines.
func superviseLoops() int {
	var buf bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
	return strings.Count(buf.String(), "(*moduleSlot).supervise(")
}
