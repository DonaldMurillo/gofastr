package framework

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// This file implements design §6 decision C's fail-closed runner selection:
//   - SandboxRunner = baseline hygiene (shared with TrustedProcessRunner via
//     [prepareChildForSpawn]/[startPreparedChild]) + OS-enforced denials
//     layered in by a per-OS [SandboxBackend] between prepare and start.
//   - NewSandboxRunner PROBES the backend at construction (the §6 gate):
//     every probe P1–P7 MUST pass. Any failure or unreachable ⇒ the
//     constructor errors and the supervisor records the module
//     InstalledDisabled / fails Register — never a silent downgrade to
//     TrustedProcessRunner.
//   - SelectRunner maps a descriptor's TrustTier to the right Runner:
//     trusted → TrustedProcessRunner, untrusted → SandboxRunner (or
//     fail-closed error). This is the inversion of the wave-2a site that
//     unconditionally returned UntrustedNoSandboxError.
//
// The inversion of bash.go's nil-check (framework/harness/tool/builtins/
// bash.go:121-123) is deliberate and load-bearing: bash's SandboxFn is
// nil = no sandbox (fails OPEN, correct for a first-party dev tool behind
// a blocklist); a nil/unconforming sandbox for an untrusted module fails
// CLOSED. The two seams look alike but mean opposite things.

// SandboxRunner is the wave-3a Runner for TrustUntrusted modules: baseline
// hygiene (shared with [TrustedProcessRunner]) PLUS the OS-enforced
// denials the selected [SandboxBackend] applies via Wrap. It is the only
// Runner an untrusted descriptor may run under; selection is fail-closed
// (a non-conforming backend ⇒ the constructor errors ⇒ the supervisor
// refuses Register, never a silent TrustedProcessRunner downgrade).
type SandboxRunner struct {
	// backend is the per-OS confinement wrapper selected at construction.
	// Immutable after NewSandboxRunner returns.
	backend SandboxBackend

	// opts are the default per-module confinement parameters. Per-spec
	// overrides (a descriptor with tighter Limits) narrow these at Start;
	// they never widen — a descriptor cannot raise a ceiling.
	opts SandboxOpts

	// EnvAllowlist is the base env-var names the child receives (PATH,
	// HOME, …). If nil, [DefaultChildEnvAllowlist] is used. This is the
	// SAME baseline-hygiene allowlist TrustedProcessRunner applies — the
	// two runners share it so P2 (no inherited secret) cannot drift.
	EnvAllowlist []string

	// NewCodec constructs a Codec over the supplied reader/writer. If nil,
	// [moduleproto.NewCodec] is used with the spec's negotiated frame
	// bytes. Mirrors TrustedProcessRunner.NewCodec for test injection.
	NewCodec func(r io.Reader, w io.Writer, maxFrameBytes int) (*moduleproto.Codec, error)

	// report is the cached conformance pass from construction. Conforms()
	// reads this; it is the fail-closed gate the supervisor consults.
	report ConformanceReport
}

// NewSandboxRunner constructs a SandboxRunner over backend, PROBING the
// backend at construction per design §6. Returns a non-nil error iff the
// backend is unavailable or any probe P1–P7 fails or is unreachable —
// the caller (supervisor wiring) treats that as "no sandbox on this host"
// and fail-closes untrusted Register attempts.
//
// The probe pass runs the full suite once and caches the report on the
// returned runner; subsequent Start calls do NOT re-probe (the backend's
// enforcement capability is a property of the host, not per-spawn).
func NewSandboxRunner(backend SandboxBackend, opts SandboxOpts) (*SandboxRunner, error) {
	if backend == nil {
		return nil, fmt.Errorf("%w: nil backend", ErrSandboxUnavailable)
	}
	if !backend.Available() {
		return nil, fmt.Errorf("%w: backend %s unavailable: %s", ErrSandboxUnavailable, backend.Name(), backend.MissingReason())
	}
	// Probe at construction (§6 fail-closed gate). Two-minute budget is
	// generous: the suite spawns seven short-lived children; even a
	// hung dial (P4) is bounded by the per-probe timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := RunConformance(ctx, backend, nil)
	if !report.Conforms() {
		return nil, fmt.Errorf("%w: backend %s does not conform (every probe P1-P7 must pass):\n%s",
			ErrSandboxUnavailable, backend.Name(), report.Summary())
	}
	return &SandboxRunner{backend: backend, opts: opts, report: report}, nil
}

// Start implements [Runner.Start]. It is the untrusted-tier spawn:
// baseline hygiene (shared [prepareChildForSpawn]) + backend.Wrap (the
// OS-enforced denials) + shared [startPreparedChild]. The backend.Wrap
// step is the ONLY difference from TrustedProcessRunner.Start — that is
// the design's point: the two runners are identical except for the
// OS-enforced denial layer.
func (r *SandboxRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	prep, err := prepareChildForSpawn(spec, r.allowlist())
	if err != nil {
		return nil, err
	}
	// Build the per-spawn confinement opts from the runner defaults,
	// narrowed by the descriptor's Limits (never widened — a descriptor
	// may lower a ceiling, not raise it).
	opts := r.opts
	if opts.ScratchDir == "" {
		opts.ScratchDir = prep.Scratch
	}
	if prep.Spec.Descriptor.ArtifactPath != "" {
		opts.PackageDir = filepath.Dir(prep.Spec.Descriptor.ArtifactPath)
	}
	// Apply OS-enforced denials on top of baseline hygiene. A Wrap error
	// is fail-closed: we do NOT exec the child unconfined.
	if err := r.backend.Wrap(prep.Cmd, opts); err != nil {
		cleanupPrepPipes(prep)
		return nil, fmt.Errorf("processmodule: sandbox %s wrap: %w", r.backend.Name(), err)
	}
	return startPreparedChild(ctx, prep, r.NewCodec)
}

// Conforms reports whether the backend passed every probe P1–P7 at
// construction. The supervisor consults this at Register to fail-closed
// an untrusted module whose host lost conformance (e.g. the operator
// pointed NewSandboxRunner at a backend that was available at boot but
// is now not — belt-and-suspenders alongside the constructor's gate).
func (r *SandboxRunner) Conforms() bool {
	if r == nil {
		return false
	}
	return r.report.Conforms()
}

// Report returns the cached conformance report from construction.
// Operator-facing surfaces (introspection, install UI) read this to show
// WHY an untrusted module fail-closed on this host.
func (r *SandboxRunner) Report() ConformanceReport {
	if r == nil {
		return ConformanceReport{Backend: "none", Available: false, MissingReason: "no sandbox runner configured"}
	}
	return r.report
}

// Backend returns the selected backend (for diagnostics / introspection).
func (r *SandboxRunner) Backend() SandboxBackend {
	if r == nil {
		return nil
	}
	return r.backend
}

// allowlist returns the runner's env allowlist, defaulting to
// DefaultChildEnvAllowlist — mirrors TrustedProcessRunner.allowlist so
// the two runners share the exact same baseline hygiene.
func (r *SandboxRunner) allowlist() []string {
	if r == nil || len(r.EnvAllowlist) == 0 {
		return DefaultChildEnvAllowlist()
	}
	return r.EnvAllowlist
}

// cleanupPrepPipes closes the three stdio pipes prepareChildForSpawn
// opened, used on the Wrap-error fail-closed path so the parent does not
// leak pipe fds across a failed spawn.
func cleanupPrepPipes(prep *childPrep) {
	if prep == nil {
		return
	}
	_ = prep.Stdin.Close()
	_ = prep.Stdout.Close()
	_ = prep.StderrPipe.Close()
}

// SelectRunner maps a descriptor's TrustTier to the Runner the supervisor
// spawns under (design §6 decision C — the inversion of the wave-2a
// site that unconditionally errored on TrustUntrusted):
//
//   - TrustTrusted → trusted (TrustedProcessRunner).
//   - TrustUntrusted + sandbox that Conforms() → sandbox (SandboxRunner).
//   - TrustUntrusted + nil/non-conforming sandbox → ErrSandboxUnavailable
//     (the caller — supervisor.Register — wraps this as
//     [UntrustedNoSandboxError] and the module never reaches Ready).
//
// This is the single point that decides runner selection; the supervisor
// calls it once at Register and stores the result on the moduleSlot.
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
