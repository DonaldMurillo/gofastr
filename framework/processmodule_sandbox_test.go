package framework

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

//
//   - TestSelectRunner_*  — the fail-closed selection logic (design §6
//     decision C): trusted → trusted runner; untrusted + conforming
//     sandbox → sandbox; untrusted + nil/non-conforming → error.
//   - TestNewSandboxRunner_* — the constructor's probe-at-construction
//     gate: nil/unavailable/non-conforming backend ⇒ error.
//   - TestSupervisor_UntrustedFailsClosedNoSandbox — the inverted
//     supervisor site still fails closed when no sandbox is configured
//     (preserves the wave-2a contract; the inversion is that it CAN now
//     succeed when a conforming sandbox is supplied).
//   - TestSupervisor_UntrustedRegistersWithConformingSandbox — the
//     inversion's positive case: an untrusted descriptor registers when
//     SupervisorConfig.Sandbox is a probe-passing runner.

// fakeBackend is a test double for SandboxBackend. It does NOT confine;
// tests inject it to drive the selection / constructor logic without
// depending on a real bwrap/sandbox-exec install. The conformance-suite
// end-to-end test (TestSandboxConformance in processmodule_probe_test.go)
// exercises the REAL host backend.
type fakeBackend struct {
	name      string
	available bool
	missing   string
	declared  []ProbeID
	wrapErr   error
	wrapped   int
}

func (f *fakeBackend) Name() string              { return f.name }
func (f *fakeBackend) Available() bool           { return f.available }
func (f *fakeBackend) MissingReason() string     { return f.missing }
func (f *fakeBackend) DeclaredProbes() []ProbeID { return f.declared }
func (f *fakeBackend) Wrap(_ *exec.Cmd, _ SandboxOpts) error {
	f.wrapped++
	return f.wrapErr
}

// newFakeConformingSandbox builds a *SandboxRunner whose Conforms() is
// true WITHOUT running the real probe suite — the report is hand-built
// all-pass. Used to test selection / supervisor wiring in isolation from
// host sandbox availability.
func newFakeConformingSandbox(b SandboxBackend) *SandboxRunner {
	results := make([]ProbeResult, 0, len(allProbes))
	for _, p := range allProbes {
		results = append(results, ProbeResult{ID: p, Status: ProbeStatusPass})
	}
	return &SandboxRunner{
		backend: b,
		report: ConformanceReport{
			Backend:   backendName(b),
			Available: true,
			Results:   results,
		},
	}
}

// newFakeNonConformingSandbox builds a *SandboxRunner whose Conforms() is
// false (P1 breached). Used to test the belt-and-suspenders path: even a
// non-nil SandboxRunner whose backend stopped conforming fail-closes.
func newFakeNonConformingSandbox(b SandboxBackend) *SandboxRunner {
	results := make([]ProbeResult, 0, len(allProbes))
	for _, p := range allProbes {
		status := ProbeStatusPass
		if p == ProbeDistinctPrincipal {
			status = ProbeStatusFail
		}
		results = append(results, ProbeResult{ID: p, Status: status})
	}
	return &SandboxRunner{
		backend: b,
		report: ConformanceReport{
			Backend:   backendName(b),
			Available: true,
			Results:   results,
		},
	}
}

func TestSelectRunner_TrustedReturnsTrusted(t *testing.T) {
	trusted := &TrustedProcessRunner{}
	got, err := SelectRunner(TrustTrusted, trusted, nil)
	if err != nil {
		t.Fatalf("trusted selection: unexpected err %v", err)
	}
	if got != trusted {
		t.Errorf("trusted selection: want trusted runner, got %T", got)
	}
}

func TestSelectRunner_UntrustedNoSandboxFails(t *testing.T) {
	trusted := &TrustedProcessRunner{}
	_, err := SelectRunner(TrustUntrusted, trusted, nil)
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("untrusted + nil sandbox: want ErrSandboxUnavailable, got %v", err)
	}
}

func TestSelectRunner_UntrustedNonConformingFails(t *testing.T) {
	trusted := &TrustedProcessRunner{}
	nc := newFakeNonConformingSandbox(&fakeBackend{name: "fake", available: true})
	_, err := SelectRunner(TrustUntrusted, trusted, nc)
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("untrusted + non-conforming: want ErrSandboxUnavailable, got %v", err)
	}
}

func TestSelectRunner_UntrustedConformingReturnsSandbox(t *testing.T) {
	trusted := &TrustedProcessRunner{}
	sb := newFakeConformingSandbox(&fakeBackend{name: "fake", available: true})
	got, err := SelectRunner(TrustUntrusted, trusted, sb)
	if err != nil {
		t.Fatalf("untrusted + conforming: unexpected err %v", err)
	}
	if got != sb {
		t.Errorf("untrusted + conforming: want sandbox runner, got %T", got)
	}
}

func TestSelectRunner_NilTrustedErrors(t *testing.T) {
	_, err := SelectRunner(TrustTrusted, nil, nil)
	if err == nil {
		t.Fatal("trusted selection with nil trusted runner: want err")
	}
}

func TestNewSandboxRunner_NilBackendErrors(t *testing.T) {
	_, err := NewSandboxRunner(nil, SandboxOpts{})
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("nil backend: want ErrSandboxUnavailable, got %v", err)
	}
}

func TestNewSandboxRunner_UnavailableBackendErrors(t *testing.T) {
	b := &fakeBackend{name: "fake", available: false, missing: "not installed"}
	_, err := NewSandboxRunner(b, SandboxOpts{})
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("unavailable backend: want ErrSandboxUnavailable, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "not installed") {
		t.Errorf("unavailable backend: err should name the missing reason; got %v", err)
	}
}

// TestNewSandboxRunner_RealBackendProbesOrFail exercises the constructor's
// probe-at-construction gate against whatever backend the host actually
// has. On every host it asserts the constructor returns EITHER a working
// runner OR an error mentioning ErrSandboxUnavailable — never a silent
// nil-both. A host whose backend does not conform (the v1 norm on
// darwin/windows/some-linux) sees the constructor error; the error
// message MUST contain the report summary so the operator can see which
// probe failed.
func TestNewSandboxRunner_RealBackendProbesOrFail(t *testing.T) {
	b := HostSandboxBackend()
	if b == nil {
		t.Skip("host has no sandbox backend compiled in / available; nothing to construct")
	}
	r, err := NewSandboxRunner(b, SandboxOpts{})
	if err != nil {
		if !errors.Is(err, ErrSandboxUnavailable) {
			t.Fatalf("constructor error not ErrSandboxUnavailable: %v", err)
		}
		// Non-conforming host: the error MUST carry the report summary
		// so the operator sees which probes failed/unreachable.
		if !strings.Contains(err.Error(), "NOT CONFORMING") && !strings.Contains(err.Error(), "does not conform") {
			t.Errorf("non-conforming constructor error should include report summary; got: %v", err)
		}
		t.Logf("host backend %s does not conform (expected on darwin/windows v1): %v", b.Name(), err)
		return
	}
	if r == nil || !r.Conforms() {
		t.Fatalf("constructor returned non-conforming runner without error: r=%v conforms=%v", r, r.Conforms())
	}
	t.Logf("host backend %s CONFORMS — untrusted modules can run", b.Name())
}

// TestSandboxRunner_StartAppliesBackendWrap asserts that SandboxRunner.Start
// calls backend.Wrap exactly once on the prepared cmd (the single
// difference from TrustedProcessRunner.Start). It uses a fake backend
// whose Wrap records the invocation; the child is NOT actually exec'd
// (the descriptor pins a nonexistent artifact, so prepareChildForSpawn
// errors at the SHA-256 step — but that is AFTER the backend would wrap;
// to test wrap-ordering we point the artifact at a real file).
func TestSandboxRunner_StartAppliesBackendWrap(t *testing.T) {
	// Build a real (tiny) artifact so prepareChildForSpawn's SHA check
	// passes and we reach the Wrap step.
	dir := t.TempDir()
	path := dir + "/child"
	if err := exec.Command("cp", testBinaryPath(t), path).Run(); err != nil {
		t.Fatalf("cp test binary: %v", err)
	}
	sha, err := sha256OfFile(path)
	if err != nil {
		t.Fatalf("sha: %v", err)
	}

	b := &fakeBackend{name: "fake", available: true, declared: allProbes}
	r := newFakeConformingSandbox(b)
	rc, err := r.Start(context.Background(), ChildSpec{
		Descriptor: ProcessModuleDescriptor{
			Name:           "t",
			Version:        "1",
			ArtifactPath:   path,
			ArtifactSHA256: sha,
			TrustTier:      TrustUntrusted,
		},
		ScratchDir: dir,
	})
	if err != nil {
		// The fake child (a copy of the test binary) will start, speak
		// no moduleproto, and the codec wire may fail; that is fine for
		// this test — we only assert Wrap fired. Kill anything that
		// started.
		if rc != nil {
			_ = rc.Kill()
			_ = rc.Wait()
		}
	}
	if b.wrapped == 0 {
		t.Fatal("SandboxRunner.Start did not call backend.Wrap")
	}
	// Reap any half-spawned child to keep the test suite clean.
	if rc != nil {
		_ = rc.Kill()
		_ = rc.Wait()
	}
}

// testBinaryPath returns the path to the running test binary so a test
// can copy it elsewhere as a stand-in artifact.
func testBinaryPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return exe
}
