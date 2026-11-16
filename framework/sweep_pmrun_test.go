package framework

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

type sweepPMRunPanicRunner struct{}

func (sweepPMRunPanicRunner) Start(context.Context, ChildSpec) (RunningChild, error) {
	panic("runner boom")
}

type sweepPMRunPanicBroker struct{ NopBroker }

func (sweepPMRunPanicBroker) InstallHandlers(*moduleproto.Peer, ModuleGrantView) {
	panic("broker boom")
}

type sweepPMRunPanicRegistrar struct{}

func (sweepPMRunPanicRegistrar) RegisterTools(string, []moduleproto.Tool) error {
	panic("registrar boom")
}
func (sweepPMRunPanicRegistrar) UnregisterTools(string) error { return nil }

type sweepPMRunPanicStore struct{ *SQLProcessModuleStore }

func (sweepPMRunPanicStore) GetDesired(context.Context, string) (DesiredState, error) {
	panic("store boom")
}

func (sweepPMRunPanicStore) RecordHeartbeat(context.Context, string, string, uint64, string) error {
	panic("heartbeat boom")
}

// A panic in the swappable runner, broker, tool registrar or store comes
// back as that call's error and never escapes the supervisor goroutine.
func TestSweepPMRunPanicsBecomeErrors(t *testing.T) {
	store := sweepPMRunPanicStore{newTestStore(t)}
	sup, err := NewProcessModuleSupervisor(SupervisorConfig{
		Store:         store,
		Runner:        sweepPMRunPanicRunner{},
		Broker:        sweepPMRunPanicBroker{},
		ToolRegistrar: sweepPMRunPanicRegistrar{},
		Logf:          safeLogf(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	sl := &moduleSlot{name: "demo", sup: sup, runner: sup.runner}

	if child, err := sl.startChildSafely(context.Background(), ChildSpec{}); child != nil || err == nil || !strings.Contains(err.Error(), "runner Start panic") {
		t.Errorf("runner panic: child=%v err=%v, want nil child and a runner Start panic error", child, err)
	}
	if err := sl.installHandlersSafely(nil, ModuleGrantView{}); err == nil || !strings.Contains(err.Error(), "broker InstallHandlers panic") {
		t.Errorf("broker panic: err=%v, want a broker InstallHandlers panic error", err)
	}
	if err := sl.registerToolsSafely("demo", nil); err == nil || !strings.Contains(err.Error(), "tools RegisterTools panic") {
		t.Errorf("registrar panic: err=%v, want a tools RegisterTools panic error", err)
	}
	if _, err := sl.getDesiredSafely(context.Background()); err == nil || !strings.Contains(err.Error(), "store GetDesired panic") {
		t.Errorf("store panic: err=%v, want a store GetDesired panic error", err)
	}
	sl.recordHeartbeatSafely(context.Background(), 1, StateReady)
}

// A store that has never answered counts as an expired lease: the module
// is drained, never served on an unread desired state.
func TestSweepPMRunNeverReadLeaseIsExpired(t *testing.T) {
	store := sweepPMRunPanicStore{newTestStore(t)}
	sup := newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	sl := &moduleSlot{name: "demo", sup: sup}
	if _, err := sl.refreshDesired(); !errors.Is(err, errLeaseExpired) {
		t.Fatalf("first failed read: err=%v, want errLeaseExpired", err)
	}
}

// The restart backoff doubles from BackoffMin per recorded crash, never
// exceeds BackoffMax plus half of it in jitter, and never drops below
// BackoffMin.
func TestSweepPMRunBackoffBounded(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	sup.cfg.BackoffMin = 10 * time.Millisecond
	sup.cfg.BackoffMax = 80 * time.Millisecond
	sl := &moduleSlot{name: "demo", sup: sup}
	for n, base := range []time.Duration{10, 20, 40, 80, 80, 80, 80} {
		base *= time.Millisecond
		sl.restarts = make([]time.Time, n)
		for range 20 {
			d := sl.nextBackoff()
			if d < base || d > base+base/2 {
				t.Fatalf("%d crashes: backoff %s outside [%s, %s]", n, d, base, base+base/2)
			}
		}
	}
	sl.restarts = make([]time.Time, 70)
	if d := sl.nextBackoff(); d < 80*time.Millisecond || d > 120*time.Millisecond {
		t.Fatalf("shift overflow: backoff %s, want BackoffMax with jitter", d)
	}
}

// A sandbox backend whose Wrap fails or panics leaves the child unexec'd,
// and the runner removes the scratch dir it made.
func TestSweepPMRunWrapFailureNeverExecs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the artifact is a shell script")
	}
	for _, tc := range []struct {
		name    string
		backend SandboxBackend
	}{
		{"error", &fakeBackend{name: "fake", available: true, declared: allProbes, wrapErr: errors.New("no confinement")}},
		{"panic", &sweepPMRunPanicWrapBackend{fakeBackend{name: "fake", available: true, declared: allProbes}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "ran")
			artifact := filepath.Join(dir, "child.sh")
			if err := os.WriteFile(artifact, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			sha, err := sha256OfFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			r := newFakeConformingSandbox(tc.backend)
			name := "sweepwrap" + tc.name
			rc, err := r.Start(context.Background(), ChildSpec{
				Descriptor: ProcessModuleDescriptor{Name: name, Version: "1", ArtifactPath: artifact, ArtifactSHA256: sha, TrustTier: TrustUntrusted},
				InstanceID: "sweep-wrap-" + tc.name,
			})
			if rc != nil {
				_ = rc.Kill()
				_ = rc.Wait()
			}
			if err == nil || !strings.Contains(err.Error(), "wrap") {
				t.Fatalf("Start err=%v, want a wrap error", err)
			}
			if _, statErr := os.Stat(marker); statErr == nil {
				t.Fatal("the child ran although Wrap refused to confine it")
			}
			scratch := filepath.Join(os.TempDir(), "gofastr-module-"+name+"-sweep-wrap-")
			matches, _ := filepath.Glob(scratch + "*")
			if len(matches) != 0 {
				t.Fatalf("scratch dir left behind after a failed wrap: %v", matches)
			}
		})
	}
}

type sweepPMRunPanicWrapBackend struct{ fakeBackend }

func (b *sweepPMRunPanicWrapBackend) Wrap(*exec.Cmd, SandboxOpts) error {
	panic("wrap boom")
}

// An artifact whose bytes do not hash to the descriptor's pin is refused
// with ExecutableSHAMismatchError before exec, under both runners.
func TestSweepPMRunDigestMismatchNeverExecs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the artifact is a shell script")
	}
	for _, tc := range []struct {
		name   string
		runner Runner
	}{
		{"trusted", &TrustedProcessRunner{}},
		{"sandbox", newFakeConformingSandbox(&fakeBackend{name: "fake", available: true, declared: allProbes})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "ran")
			artifact := filepath.Join(dir, "child.sh")
			if err := os.WriteFile(artifact, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			rc, err := tc.runner.Start(context.Background(), ChildSpec{
				Descriptor: ProcessModuleDescriptor{Name: "sweepsha", Version: "1", ArtifactPath: artifact, ArtifactSHA256: strings.Repeat("ab", 32)},
				InstanceID: "sweep-sha-" + tc.name,
				ScratchDir: t.TempDir(),
			})
			if rc != nil {
				_ = rc.Wait()
			}
			if _, ok := errors.AsType[*ExecutableSHAMismatchError](err); !ok {
				t.Fatalf("Start err=%v, want *ExecutableSHAMismatchError", err)
			}
			if _, statErr := os.Stat(marker); statErr == nil {
				t.Fatal("an artifact that fails its digest pin ran")
			}
		})
	}
}

// The child environment holds only allowlisted, inherited and explicit
// names: a host variable on none of those lists never reaches the child.
func TestSweepPMRunChildEnvIsAllowlistOnly(t *testing.T) {
	t.Setenv("GOFASTR_SWEEP_HOST_SECRET", "hunter2")
	env := buildChildEnv(DefaultChildEnvAllowlist(), []string{"EXPLICIT=1"}, []string{"PATH"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOFASTR_SWEEP_HOST_SECRET=") {
			t.Fatalf("a host variable outside the allowlist reached the child env: %v", env)
		}
	}
	if !slices.Contains(env, "EXPLICIT=1") {
		t.Fatalf("explicit entry missing from child env: %v", env)
	}
}

// An exec that fails closes the verified artifact fd prepare opened.
func TestSweepPMRunExecFailClosesArtifact(t *testing.T) {
	if !fdExecSupported {
		t.Skip("exec by path: prepare keeps no artifact fd")
	}
	path, sha := buildChildArtifact(t)
	prep, err := prepareChildForSpawn(ChildSpec{
		Descriptor: ProcessModuleDescriptor{Name: "execfail", Version: "1", ArtifactPath: path, ArtifactSHA256: sha},
		InstanceID: "exec-fail-instance",
		ScratchDir: t.TempDir(),
	}, DefaultChildEnvAllowlist())
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	prep.Cmd.Dir = filepath.Join(t.TempDir(), "missing")
	if rc, err := startPreparedChild(context.Background(), prep, nil); err == nil || !strings.Contains(err.Error(), "exec") {
		if rc != nil {
			_ = rc.Kill()
			_ = rc.Wait()
		}
		t.Fatalf("start err = %v, want an exec error", err)
	}
	if err := prep.Artifact.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("artifact fd still open after a failed exec (Close = %v)", err)
	}
}

type sweepPMRunRefusingRunner struct{}

func (sweepPMRunRefusingRunner) Start(context.Context, ChildSpec) (RunningChild, error) {
	return nil, errors.New("no spawn in this test")
}

// A Failed module starts no spawn on reconcile until its generation
// advances; the bump then starts one.
func TestSweepPMRunFailedWaitsForGenerationBump(t *testing.T) {
	ctx := context.Background()
	sup := newBareTestSupervisor(t, newTestStore(t), sweepPMRunRefusingRunner{})
	d := validDescriptor()
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := sup.store.SetEnabled(ctx, d.Name, true); err != nil {
		t.Fatal(err)
	}
	sl := sup.Slot(d.Name)
	sl.mu.Lock()
	sl.desiredGen = 1
	sl.state = StateFailed
	seq := sl.spawnSeq
	sl.mu.Unlock()
	for range 3 {
		sl.reconcile()
	}
	sl.mu.RLock()
	got, state := sl.spawnSeq, sl.state
	sl.mu.RUnlock()
	if got != seq || state != StateFailed {
		t.Fatalf("reconcile of a Failed module without a bump: spawnSeq %d -> %d, state %s; want no spawn and Failed", seq, got, state)
	}
	if _, err := sup.store.BumpGeneration(ctx, d.Name); err != nil {
		t.Fatal(err)
	}
	sl.reconcile()
	sl.mu.RLock()
	got = sl.spawnSeq
	sl.mu.RUnlock()
	if got == seq {
		t.Fatal("a generation bump did not re-arm the Failed module")
	}
}

// A child whose module.tool.list omits a descriptor tool, or lists one id
// twice, fails the tool surface check with a handshake mismatch.
func TestSweepPMRunToolSurfaceMissingOrDuplicate(t *testing.T) {
	search := demoToolDigest("search", "Search", "desc", nil)
	fetch := demoToolDigest("fetch", "Fetch", "desc", nil)
	for name, listed := range map[string][]moduleproto.Tool{
		"missing":   {{ID: "search", Name: "Search", Description: "desc"}},
		"duplicate": {{ID: "search", Name: "Search", Description: "desc"}, {ID: "search", Name: "Search", Description: "desc"}, {ID: "fetch", Name: "Fetch", Description: "desc"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, sl, host, child, cleanup := toolTestSlot(t, []ToolDigest{search, fetch})
			defer cleanup()
			if err := child.Handle(moduleproto.MethodToolList, func(context.Context, json.RawMessage) (any, error) {
				return moduleproto.ToolListResult{Tools: listed}, nil
			}); err != nil {
				t.Fatalf("handle: %v", err)
			}
			err := sl.verifyToolSurface(context.Background(), host)
			mm, ok := errors.AsType[*moduleproto.HandshakeMismatchError](err)
			if !ok || mm.Field != "module.tool.list" {
				t.Fatalf("want a module.tool.list HandshakeMismatchError, got %T %v", err, err)
			}
		})
	}
}

// An upgrade drain that cannot re-read the store leaves the module
// InstalledDisabled and starts no spawn with the old grants.
func TestSweepPMRunUpgradeDrainFailsClosedOnStoreError(t *testing.T) {
	ctx := context.Background()
	sup := newBareTestSupervisor(t, sweepPMRunPanicStore{newTestStore(t)}, sweepPMRunRefusingRunner{})
	d := validDescriptor()
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.leaseMu.Lock()
	sup.lease[d.Name] = sup.now()
	sup.leaseMu.Unlock()
	sl := sup.Slot(d.Name)
	sl.mu.RLock()
	seq := sl.spawnSeq
	sl.mu.RUnlock()
	sl.finishDrain(StateDrainingUpgrade, 2)
	sl.mu.RLock()
	got, state := sl.spawnSeq, sl.state
	sl.mu.RUnlock()
	if got != seq || state != StateInstalledDisabled {
		t.Fatalf("upgrade drain on an unreadable store: spawnSeq %d -> %d, state %s; want no spawn and InstalledDisabled", seq, got, state)
	}
}
