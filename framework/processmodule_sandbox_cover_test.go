package framework

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// the SelectRunner branches + cleanupPrepPipes in processmodule_sandbox.go
// that are reachable WITHOUT spawning or running the real conformance suite.

// ---- nil-safe accessors ----

func TestSandboxRunner_NilAccessors(t *testing.T) {
	var r *SandboxRunner
	if r.Conforms() {
		t.Error("nil Conforms() must be false")
	}
	if r.Backend() != nil {
		t.Error("nil Backend() must return nil")
	}
	rep := r.Report()
	if rep.Available || rep.Backend != "none" {
		t.Errorf("nil Report() = %+v, want unavailable none", rep)
	}
	if got := r.allowlist(); len(got) == 0 {
		t.Error("nil allowlist() must fall back to default, not empty")
	}
}

// ---- allowlist falls back to default when empty ----

func TestSandboxRunner_AllowlistFallsBack(t *testing.T) {
	r := &SandboxRunner{} // EnvAllowlist empty
	got := r.allowlist()
	if len(got) == 0 {
		t.Error("empty EnvAllowlist must fall back to default, not empty")
	}
	// A custom allowlist is returned as-is.
	r.EnvAllowlist = []string{"CUSTOM"}
	got = r.allowlist()
	if len(got) != 1 || got[0] != "CUSTOM" {
		t.Errorf("custom allowlist = %+v", got)
	}
}

// ---- Report / Backend on a conforming runner (non-nil branches) ----

func TestSandboxRunner_ReportAndBackendNonNil(t *testing.T) {
	backend := &fakeBackend{name: "fake", available: true, declared: allProbes}
	r := newFakeConformingSandbox(backend)
	rep := r.Report()
	if !rep.Available || rep.Backend != "fake" {
		t.Errorf("Report() = %+v", rep)
	}
	if got := r.Backend(); got == nil || got.Name() != "fake" {
		t.Errorf("Backend() = %+v", got)
	}
}

// ---- cleanupPrepPipes (nil-safe + closes real pipes) ----

func TestCleanupPrepPipes_nilPointerIsNoOp(t *testing.T) {
	// The function is nil-safe only on the pointer; a zero childPrep{} has
	// nil interface pipe fields whose .Close() would panic, so callers
	// always pass either nil or a fully-populated prep.
	cleanupPrepPipes(nil)
}

// ---- SelectRunner: trusted with nil trusted errors ----

func TestSelectRunner_TrustedNilErrors(t *testing.T) {
	// SelectRunner(TrustTrusted, nil, nil) must error (covered already in
	// sandbox_test.go as TestSelectRunner_NilTrustedErrors). We add the
	// unknown-tier branch here.
	if _, err := SelectRunner(TrustTier(99), &TrustedProcessRunner{}, nil); err == nil {
		t.Error("unknown trust tier must error")
	}
}

// ---- SandboxRunner.Start fail-closed path: SHA mismatch before Wrap ----

func TestSandboxRunner_StartSHAErrorBeforeWrap(t *testing.T) {
	// A conforming sandbox + a descriptor whose artifact path is nonexistent.
	// prepareChildForSpawn verifies the SHA pin BEFORE the backend.Wrap step,
	// so Start returns an error and Wrap is never invoked.
	backend := &fakeBackend{name: "fake", available: true}
	r := newFakeConformingSandbox(backend)
	spec := ChildSpec{
		Descriptor: validDescriptorForRunner(), // pins a nonexistent artifact
		Stderr:     moduleproto.NewRingSink(64),
	}
	_, err := r.Start(context.Background(), spec)
	if err == nil {
		t.Fatal("SandboxRunner.Start with missing artifact must error")
	}
	if backend.wrapped != 0 {
		t.Errorf("backend.Wrap called %d times; SHA check must run BEFORE Wrap", backend.wrapped)
	}
}

// ---- ExecutableSHAMismatchError satisfies the integrity-fault shape ----

func TestExecutableSHAMismatchError_isIntegrity(t *testing.T) {
	e := &ExecutableSHAMismatchError{Path: "x", Expected: "a", Actual: "b"}
	var target *ExecutableSHAMismatchError
	if !errors.As(e, &target) {
		t.Error("errors.As must match *ExecutableSHAMismatchError")
	}
}

// validDescriptorForRunner builds a minimal valid descriptor whose artifact
// path is a guaranteed-missing file so prepareChildForSpawn's SHA check fails
// predictably. It is distinct from the shared validDescriptor() so this file
// stays self-contained.
func validDescriptorForRunner() ProcessModuleDescriptor {
	d := ProcessModuleDescriptor{
		Name:           "demo",
		Version:        "1.0.0",
		ArtifactPath:   "/this/path/does/not/exist/gofastr-test",
		ArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SurfaceSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Routes: []RouteDeclaration{
			{ID: "r1", Method: "GET", Path: "/r"},
		},
		RequestedGrants: []access.Permission{"articles:read"},
		TrustTier:       TrustTrusted,
	}
	return d
}

// keep exec referenced (childPrep carries *exec.Cmd; the SHA-error test builds
// a spec that prepareChildForSpawn turns into an exec.Cmd before the SHA fail).
var _ = exec.Command
