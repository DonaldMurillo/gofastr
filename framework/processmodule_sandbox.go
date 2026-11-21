package framework

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// SandboxRunner launches untrusted modules: the trusted runner's hygiene plus the
// OS-enforced confinement its backend's Wrap applies.
type SandboxRunner struct {
	backend SandboxBackend
	opts    SandboxOpts

	// EnvAllowlist names the host variables the child gets. An empty list uses
	// [DefaultChildEnvAllowlist].
	EnvAllowlist []string

	// NewCodec builds the protocol codec; nil uses [moduleproto.NewCodec].
	NewCodec func(r io.Reader, w io.Writer, maxFrameBytes int) (*moduleproto.Codec, error)
	report   ConformanceReport
}

// NewSandboxRunner probes backend with the full conformance suite and returns a
// runner only when every probe passes, else a [SandboxConformanceError].
func NewSandboxRunner(backend SandboxBackend, opts SandboxOpts) (*SandboxRunner, error) {
	if backend == nil {
		return nil, &SandboxConformanceError{
			Report: RunConformance(context.Background(), nil, nil),
			reason: "nil backend",
		}
	}
	if !backend.Available() {
		return nil, &SandboxConformanceError{
			Report: RunConformance(context.Background(), backend, nil),
			reason: fmt.Sprintf("backend %s unavailable: %s", backend.Name(), backend.MissingReason()),
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := RunConformance(ctx, backend, nil)
	if !report.Conforms() {
		return nil, &SandboxConformanceError{
			Report: report,
			reason: fmt.Sprintf("backend %s does not conform (every probe P1-P7 must pass):\n%s", backend.Name(), report.Summary()),
		}
	}
	return &SandboxRunner{backend: backend, opts: opts, report: report}, nil
}

// SandboxConformanceError is the refusal to run untrusted modules on this host;
// it unwraps to [ErrSandboxUnavailable].
type SandboxConformanceError struct {
	// Report is the probe outcome; every probe is Unreachable for a missing backend.
	Report ConformanceReport
	reason string
}

func (e *SandboxConformanceError) Error() string {
	return ErrSandboxUnavailable.Error() + ": " + e.reason
}

// Unwrap returns [ErrSandboxUnavailable].
func (e *SandboxConformanceError) Unwrap() error { return ErrSandboxUnavailable }

// Start implements [Runner]; a Wrap error or panic fails the spawn before exec.
func (r *SandboxRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	var allowlist []string
	if r != nil {
		allowlist = r.EnvAllowlist
	}
	prep, err := prepareChildForSpawn(spec, envAllowlistOrDefault(allowlist))
	if err != nil {
		return nil, err
	}
	opts := r.opts
	if opts.ScratchDir == "" {
		opts.ScratchDir = prep.Scratch
	}
	if prep.Spec.Descriptor.ArtifactPath != "" {
		opts.PackageDir = filepath.Dir(prep.Spec.Descriptor.ArtifactPath)
	}
	if err := wrapSandboxSafely(r.backend, prep.Cmd, opts); err != nil {
		releasePrepFDs(prep)
		prep.removeOwnedScratch()
		return nil, fmt.Errorf("processmodule: sandbox %s wrap: %w", r.backend.Name(), err)
	}
	return startPreparedChild(ctx, prep, r.NewCodec)
}

func wrapSandboxSafely(backend SandboxBackend, cmd *exec.Cmd, opts SandboxOpts) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v\n%s", textsafe.Recovered(p), debug.Stack())
		}
	}()
	return backend.Wrap(cmd, opts)
}

// Conforms reports whether every probe passed at construction; false for nil.
func (r *SandboxRunner) Conforms() bool {
	if r == nil {
		return false
	}
	return r.report.Conforms()
}

// Report returns the conformance report from construction.
func (r *SandboxRunner) Report() ConformanceReport {
	if r == nil {
		return ConformanceReport{Backend: "none", Available: false, MissingReason: "no sandbox runner configured"}
	}
	return r.report
}

// Backend returns the backend, or nil.
func (r *SandboxRunner) Backend() SandboxBackend {
	if r == nil {
		return nil
	}
	return r.backend
}

func releasePrepFDs(prep *childPrep) {
	if prep == nil {
		return
	}
	_ = prep.Stdin.Close()
	_ = prep.Stdout.Close()
	_ = prep.StderrPipe.Close()
	if prep.Artifact != nil {
		_ = prep.Artifact.Close()
	}
	for _, f := range prep.ChildEnds {
		_ = f.Close()
	}
}

// SelectRunner returns the runner for tier: trusted for TrustTrusted, sandbox for
// TrustUntrusted when it conforms, else [ErrSandboxUnavailable].
func SelectRunner(tier TrustTier, trusted Runner, sandbox *SandboxRunner) (Runner, error) {
	switch tier {
	case TrustTrusted:
		if trusted == nil {
			return nil, errors.New("processmodule: no trusted runner configured")
		}
		return trusted, nil
	case TrustUntrusted:
		if sandbox == nil || !sandbox.Conforms() {
			return nil, ErrSandboxUnavailable
		}
		return sandbox, nil
	default:
		return nil, fmt.Errorf("processmodule: unknown trust tier %d", int(tier))
	}
}
