package framework

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// This file implements design §6's sandbox conformance contract: the seven
// observable-outcome probes (P1–P7) a backend MUST enforce before an
// untrusted module reaches Ready. The contract is defined by outcomes —
// "the child, run under the candidate backend, attempts the forbidden thing
// and is denied" — NOT by syscall, because Linux/Darwin/Windows reach the
// same denials by incompatible mechanisms (namespaces vs sandbox-exec vs
// AppContainer/Job Object). That shape is the only one simultaneously
// portable, testable, and CI-gateable across the three OSes.
//
// The probe suite is dual-purpose:
//   - At backend construction: NewSandboxRunner runs it to decide whether
//     the backend qualifies for an untrusted module (design §6 fail-closed:
//     every probe MUST pass; any failure or unreachable ⇒ the runner
//     constructor errors, the module never reaches Ready).
//   - As a CI gate: TestSandboxConformance runs it against the host's
//     available backend and asserts the enforceable probes actually
//     fail-to-breach. When the backend is unavailable on the host (no
//     bwrap in CI, no sandbox-exec) the test SKIPs with a clear message
//     naming what's missing — it NEVER passes silently as if enforcement
//     happened.
//
// NOTHING is written into the repo tree by the probes. The probe child IS
// the test binary itself, re-executed under an env gate
// (GOFASTR_SANDBOX_PROBE=<id>) with context env vars describing the
// planted secrets / host pid / targets. Scratch lives under t.TempDir().

// ProbeID names one of the §6 sandbox properties P1–P7. The numeric value
// matches the design's table numbering so a log line "probe=P1" is
// self-documenting.
type ProbeID int

const (
	// ProbeDistinctPrincipal is P1: the child runs as a distinct OS
	// principal from the host (uid/SID ≠ host) and cannot signal or
	// /proc-read host pids.
	ProbeDistinctPrincipal ProbeID = iota + 1

	// ProbeNoInheritedSecret is P2: a canary planted in the host env and a
	// planted secret file pre-spawn are both invisible to the child.
	// (Baseline hygiene: empty-default env allowlist.)
	ProbeNoInheritedSecret

	// ProbeNoInheritedFD is P3: a host fd opened pre-spawn to a secret is
	// not inherited — the child enumerating fds > 2 finds none.
	// (Baseline hygiene: ExtraFiles=nil.)
	ProbeNoInheritedFD

	// ProbeNoNetworkEgress is P4: connect() to loopback, a public IP, and
	// the cloud-metadata IP all fail. (Backend-enforced.)
	ProbeNoNetworkEgress

	// ProbeFilesystemConfinement is P5: the child can read only its
	// package; writes land only under scratch; host tree/$HOME/secrets
	// are unreadable. (Backend-enforced.)
	ProbeFilesystemConfinement

	// ProbeResourceLimits is P6: a fork-bomb is capped, an OOM kills the
	// child (not the host), CPU is throttled, fds are capped.
	// (Backend-enforced via cgroup/Job Object.)
	ProbeResourceLimits

	// ProbeNoPrivReEscalation is P7: the child cannot setuid up, cannot
	// gain new caps, and no-new-privileges is in effect.
	// (Backend-enforced.)
	ProbeNoPrivReEscalation
)

// allProbes is the full P1–P7 set, in order. Iterating this slice is the
// canonical way to run a complete conformance pass.
var allProbes = []ProbeID{
	ProbeDistinctPrincipal,
	ProbeNoInheritedSecret,
	ProbeNoInheritedFD,
	ProbeNoNetworkEgress,
	ProbeFilesystemConfinement,
	ProbeResourceLimits,
	ProbeNoPrivReEscalation,
}

// String renders a ProbeID as its design-table label ("P1".."P7").
func (p ProbeID) String() string {
	if p >= 1 && int(p) <= len(allProbes) {
		return fmt.Sprintf("P%d", int(p))
	}
	return fmt.Sprintf("Probe(%d)", int(p))
}

// Title is the human-readable property name from the §6 table.
func (p ProbeID) Title() string {
	switch p {
	case ProbeDistinctPrincipal:
		return "Distinct OS principal"
	case ProbeNoInheritedSecret:
		return "No inherited secret"
	case ProbeNoInheritedFD:
		return "No inherited fd"
	case ProbeNoNetworkEgress:
		return "No network egress"
	case ProbeFilesystemConfinement:
		return "Filesystem confinement"
	case ProbeResourceLimits:
		return "Resource limits"
	case ProbeNoPrivReEscalation:
		return "No privilege re-escalation"
	default:
		return p.String()
	}
}

// ProbeStatus is the outcome of one probe under one backend on one host.
type ProbeStatus int

const (
	// ProbeStatusUnknown is the zero value — a probe that has not been run.
	ProbeStatusUnknown ProbeStatus = iota

	// ProbeStatusPass means the sandbox DENIED the forbidden action: the
	// child attempted it and was refused. This is the only status that
	// counts toward conformance.
	ProbeStatusPass

	// ProbeStatusFail means the child BREACHED: the forbidden action
	// succeeded. The sandbox is not enforcing this property on this host.
	ProbeStatusFail

	// ProbeStatusUnreachable means the probe could not be meaningfully run
	// (backend unavailable, host lacks the prerequisite feature, the
	// backend's wrapper tool is missing). Unreachable ⇒ NOT conforming;
	// the distinction from Fail is diagnostic only.
	ProbeStatusUnreachable
)

// String renders a ProbeStatus for logs and test output.
func (s ProbeStatus) String() string {
	switch s {
	case ProbeStatusPass:
		return "pass"
	case ProbeStatusFail:
		return "fail"
	case ProbeStatusUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}

// ProbeResult is the outcome of running one [ProbeID] against one
// [SandboxBackend] on the current host.
type ProbeResult struct {
	ID     ProbeID
	Status ProbeStatus

	// Detail is a short human-readable note: why a breach succeeded, why
	// the probe was unreachable, or the denial observed on pass. Surfaced
	// in the [ConformanceReport] for operator diagnostics and the CI
	// skip/fail message.
	Detail string
}

// ConformanceReport is the per-backend record of all seven probes. It is
// the artifact NewSandboxRunner consults to decide fail-closed and the
// artifact TestSandboxConformance emits.
type ConformanceReport struct {
	Backend string

	// Available reports whether the backend's wrapper tool was present
	// and usable at probe time (bwrap/sandbox-exec on PATH, kernel
	// features present). When false, every probe is Unreachable.
	Available bool

	// MissingReason explains why an unavailable backend is unavailable
	// (e.g. "bwrap not on PATH", "unprivileged userns disabled").
	MissingReason string

	// Results has one entry per [ProbeID] in P1..P7 order.
	Results []ProbeResult
}

// Conforms reports whether the backend qualifies for an untrusted module
// per design §6: Available AND every probe P1–P7 passed. Any failure or
// unreachable disqualifies — there is no partial conformance, and the
// supervisor fail-closes rather than silently downgrading to
// TrustedProcessRunner.
func (r ConformanceReport) Conforms() bool {
	if !r.Available || len(r.Results) < len(allProbes) {
		return false
	}
	for _, res := range r.Results {
		if res.Status != ProbeStatusPass {
			return false
		}
	}
	return true
}

// Result returns the [ProbeResult] for id, or a zero-value result if the
// probe was not run.
func (r ConformanceReport) Result(id ProbeID) ProbeResult {
	for _, res := range r.Results {
		if res.ID == id {
			return res
		}
	}
	return ProbeResult{ID: id, Status: ProbeStatusUnknown}
}

// Summary renders the report as a multi-line human-readable string for
// logs / test output. One line per probe: "P1 pass  Distinct OS principal".
func (r ConformanceReport) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "backend=%s available=%v", r.Backend, r.Available)
	if !r.Available {
		fmt.Fprintf(&b, " missing=%q", r.MissingReason)
	}
	b.WriteByte('\n')
	for _, res := range r.Results {
		fmt.Fprintf(&b, "  %s %-11s %s\n", res.ID, res.Status, res.ID.Title())
		if res.Detail != "" {
			fmt.Fprintf(&b, "    %s\n", res.Detail)
		}
	}
	if r.Conforms() {
		fmt.Fprintf(&b, "  CONFORMS (untrusted-qualified)\n")
	} else {
		fmt.Fprintf(&b, "  NOT CONFORMING (untrusted fail-closed)\n")
	}
	return b.String()
}

// SandboxOpts carries the per-module confinement parameters a backend
// applies when wrapping the child exec. Every field is host-derived from
// the operator-approved descriptor — the child never supplies these.
type SandboxOpts struct {
	// ScratchDir is the per-module writable working directory. Backends
	// confine writes here (bwrap --bind, sandbox-exec file-write* subpath,
	// Job Object inherited-handle restrictions).
	ScratchDir string

	// PackageDir is the read-only directory holding the module's artifact
	// and its static assets. Backends mount/read it read-only (bwrap
	// --ro-bind, sandbox-exec file-read* subpath).
	PackageDir string

	// ReadOnlyDirs are additional host paths the child may read but not
	// write (system libraries, locale data). Backends mount them read-only.
	ReadOnlyDirs []string

	// PidLimit caps the child's process count (P6). 0 = backend default.
	PidLimit int

	// MemoryLimitBytes caps the child's resident memory (P6). 0 = default.
	MemoryLimitBytes int64

	// CpuQuotaPermil is the CPU quota in per-mille (1000 = one full CPU).
	// 0 = backend default.
	CpuQuotaPermil int

	// FdLimit caps the child's open file descriptors (P6). 0 = default.
	FdLimit int
}

// SandboxBackend is the per-OS confinement wrapper (design §6 — v1 is a
// wrapper command, mirroring framework/harness/tool/builtins/bash.go's
// SandboxFn but with the fail-closed seam INVERTED: a nil/nil-error Wrap
// is mandatory for untrusted modules, and the constructor refuses to
// build a non-conforming runner). Each build-tagged implementation wraps
// the child exec in the OS confinement tool and declares which probes it
// can enforce on this host.
//
// The interface is portable; the implementations are not. Go supervises
// processes portably but cannot confine them portably — stdlib
// syscall.SysProcAttr exposes Linux namespace/uid-mapping fields but the
// Darwin and Windows structs lack them entirely, hence the build-tagged
// split. The split boundary is linux vs not-linux (finer than the repo's
// existing unix-vs-not-unix split in processmodule_sysproc_*.go) because
// Darwin IS unix yet lacks namespace fields.
type SandboxBackend interface {
	// Name is the backend identifier ("bwrap", "sandbox-exec",
	// "appcontainer", "stub"). Stable across runs; used in logs and the
	// ConformanceReport.
	Name() string

	// Available reports whether this backend can run on the current host:
	// wrapper tool on PATH, kernel features present, sufficient privilege.
	// When false, MissingReason explains why and Wrap returns an error.
	Available() bool

	// MissingReason is a short human-readable note explaining why an
	// unavailable backend is unavailable. Empty when Available.
	MissingReason() string

	// Wrap rewrites cmd in place to execute the child under this backend's
	// confinement. The caller has already applied baseline hygiene
	// (scrubbed env, cwd=scratch, own process group); Wrap adds the
	// OS-enforced denials on top. Returns an error iff the backend cannot
	// wrap on this host (tool invocation failure, profile generation
	// failure) — the caller treats that as fail-closed, never as "run
	// anyway without confinement".
	Wrap(cmd *exec.Cmd, opts SandboxOpts) error

	// DeclaredProbes returns the probes this backend DECLARES it can
	// enforce on this host when Available. The conformance runner still
	// executes each probe to verify the declaration holds — this list
	// drives the "what we aim to enforce" used for skip messaging when
	// the backend is unavailable. It is the honest ceiling, not a claim:
	// if a probe in this list later fails its run, the report records
	// ProbeStatusFail and the backend does not conform.
	DeclaredProbes() []ProbeID
}

// probeEnvName is the env var gating the in-test probe-child re-exec.
// Set to a ProbeID's numeric value (1..7); the test binary re-runs as the
// probe child, attempts the forbidden action for that probe, and prints
// exactly one result line to stdout before exiting.
const probeEnvName = "GOFASTR_SANDBOX_PROBE"

// Probe-child stdout protocol. The child prints exactly one line:
//
//	"PASS"               — the sandbox denied the forbidden action.
//	"BREACH <detail>"    — the child succeeded in the forbidden action.
//	"UNREACHABLE <detail>" — the probe could not be meaningfully run.
//
// The probe runner parses this line and maps it to a [ProbeStatus]. A
// child that exits without printing (killed by the sandbox mid-attempt,
// or crashed) is treated as UNREACHABLE for P6 (the limit may have fired)
// and FAIL for everything else (the sandbox did not deny the action; it
// killed the child, which is not the same as a clean denial — surface it).
const (
	probeOutPass        = "PASS"
	probeOutBreach      = "BREACH"
	probeOutUnreachable = "UNREACHABLE"
)

// RunConformance executes the full P1–P7 probe suite against b on the
// current host and returns the report. When b is unavailable, every probe
// is recorded as Unreachable with the backend's MissingReason — this is
// NOT a pass; Conforms() returns false and the supervisor fail-closes.
//
// t is used only for scratch-dir provisioning (t.TempDir); pass nil to use
// an os.MkdirTemp under the system temp dir (for non-test callers like the
// SandboxRunner constructor probing at startup).
func RunConformance(ctx context.Context, b SandboxBackend, t testingTB) ConformanceReport {
	report := ConformanceReport{Backend: backendName(b), Results: make([]ProbeResult, 0, len(allProbes))}
	if b == nil {
		report.Available = false
		report.MissingReason = "no sandbox backend configured"
		for _, p := range allProbes {
			report.Results = append(report.Results, ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "no backend"})
		}
		return report
	}
	report.Available = b.Available()
	report.MissingReason = b.MissingReason()
	if !report.Available {
		for _, p := range allProbes {
			report.Results = append(report.Results, ProbeResult{
				ID: p, Status: ProbeStatusUnreachable,
				Detail: fmt.Sprintf("backend %s unavailable: %s", b.Name(), report.MissingReason),
			})
		}
		return report
	}

	// Scratch dir for the probe children + planted secrets.
	var scratch string
	if t != nil {
		scratch = t.TempDir()
	} else {
		dir, err := os.MkdirTemp("", "gofastr-sandbox-probe-*")
		if err != nil {
			report.Available = false
			report.MissingReason = "scratch dir: " + err.Error()
			for _, p := range allProbes {
				report.Results = append(report.Results, ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: report.MissingReason})
			}
			return report
		}
		scratch = dir
		defer os.RemoveAll(scratch)
	}

	for _, p := range allProbes {
		select {
		case <-ctx.Done():
			report.Results = append(report.Results, ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "context canceled"})
		default:
		}
		report.Results = append(report.Results, runOneProbe(ctx, b, p, scratch))
	}
	return report
}

// testingTB is the narrow slice of *testing.TB RunConformance needs. Kept
// local so non-test callers (the SandboxRunner constructor) can pass nil
// without pulling testing into a production build path.
type testingTB interface {
	TempDir() string
	Helper()
}

// runOneProbe runs a single probe under backend b. It sets up the probe
// context (plants secrets, records the host pid/uid, opens a host fd to a
// secret), builds the probe child exec.Cmd with baseline hygiene applied,
// asks the backend to wrap it, runs it, and parses the result line.
func runOneProbe(ctx context.Context, b SandboxBackend, p ProbeID, scratch string) ProbeResult {
	exe, err := os.Executable()
	if err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "os.Executable: " + err.Error()}
	}

	// Per-probe scratch so concurrent runs (a future optimization) cannot
	// collide on planted-secret filenames.
	probeScratch := filepath.Join(scratch, p.String())
	if err := os.MkdirAll(probeScratch, 0o700); err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "scratch: " + err.Error()}
	}

	setup, setupErr := setupProbeContext(p, probeScratch)
	if setupErr != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "setup: " + setupErr.Error()}
	}
	// Close any host fds the setup opened (e.g. the P3 secret fd) AFTER
	// spawning — the runner does not inherit them because ExtraFiles is
	// nil, but the test process should not leak them across the suite.
	if setup.Cleanup != nil {
		defer setup.Cleanup()
	}

	cmd := exec.Command(exe)
	cmd.Dir = probeScratch
	// Baseline hygiene (§6 — applied by BOTH runners; the probe runner
	// applies the same scrub so the suite tests the runner's contract,
	// not just the backend's): an explicit env containing ONLY what the
	// probe child needs. The canary planted by setupProbeContext lives
	// in os.Environ() (set via os.Setenv so the child sees it iff the
	// runner failed to scrub) and is deliberately NOT copied here.
	cmd.Env = buildProbeChildEnv(setup)
	// No ExtraFiles ⇒ only fds 0,1,2 are inherited (P3 enforcement).
	cmd.Stdin = nil
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	setChildProcessGroup(cmd)

	// The OS-enforced denials on top of baseline hygiene:
	if err := b.Wrap(cmd, SandboxOpts{
		ScratchDir:       probeScratch,
		PackageDir:       filepath.Dir(exe),
		PidLimit:         defaultProbePidLimit,
		FdLimit:          defaultProbeFdLimit,
		MemoryLimitBytes: defaultProbeMemLimitBytes,
	}); err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "backend.Wrap: " + err.Error()}
	}

	runCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if err := cmd.Start(); err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "start: " + err.Error()}
	}
	// Ensure the child is reaped even on timeout (a sandboxed child can
	// outlive the probe's interest in it; the group kill mirrors the
	// supervisor's teardown).
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-runCtx.Done():
		_ = killProcessTree(cmd)
		<-waitErr
		return parseProbeOutput(p, "", true, out.String())
	case err := <-waitErr:
		_ = err
		return parseProbeOutput(p, out.String(), false, out.String())
	}
}

// probeTimeout caps one probe's wall time. A sandboxed child that
// deadlocks on a denied resource (e.g. a blocked connect()) must still
// produce output well under this bound; exceeding it means the probe
// cannot complete on this host.
const probeTimeout = 15 * time.Second

// defaultProbe* are the limits the probe runner asks the backend to
// enforce for P6. They are tight enough that an enforced cap fires before
// the child can do damage, and loose enough that the child can fork the
// handful of worker processes its probe body needs (P6 forks ~200 to
// observe the cap). A backend that ignores them reports BREACH.
const (
	defaultProbePidLimit      = 64
	defaultProbeFdLimit       = 32
	defaultProbeMemLimitBytes = 64 * 1024 * 1024
)

// parseProbeOutput maps the probe child's single output line to a
// [ProbeResult]. timedOut reports whether the probe hit the wall; the
// mapping differs by probe (P6 treats being killed as the limit firing =
// pass-ish, others treat it as fail).
func parseProbeOutput(p ProbeID, stdout string, timedOut bool, stderr string) ProbeResult {
	stdout = strings.TrimSpace(stdout)
	// The child prints its result line; the first line wins (any output
	// after it is diagnostic, captured in stderr/Detail).
	first := stdout
	if idx := strings.IndexByte(stdout, '\n'); idx >= 0 {
		first = strings.TrimSpace(stdout[:idx])
	}
	if timedOut {
		// P6's whole point is "the limit fires and caps the child". A
		// timeout on P6 with no clean PASS output is ambiguous; we treat
		// it as UNREACHABLE rather than silently PASS — the runner should
		// see a clean PASS line if the limit denied cleanly.
		detail := "probe timed out after " + probeTimeout.String()
		if stderr != "" {
			detail += "; stderr tail: " + tailForDetail(stderr)
		}
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: detail}
	}
	switch {
	case strings.HasPrefix(first, probeOutPass):
		return ProbeResult{ID: p, Status: ProbeStatusPass, Detail: denialDetail(p, stderr)}
	case strings.HasPrefix(first, probeOutBreach):
		return ProbeResult{ID: p, Status: ProbeStatusFail, Detail: strings.TrimSpace(strings.TrimPrefix(first, probeOutBreach))}
	case strings.HasPrefix(first, probeOutUnreachable):
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: strings.TrimSpace(strings.TrimPrefix(first, probeOutUnreachable))}
	case first == "":
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "child produced no output; stderr tail: " + tailForDetail(stderr)}
	default:
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "unrecognized output: " + first}
	}
}

// denialDetail extracts the diagnostic the child emitted alongside its
// PASS — usually the errno/string the sandbox returned, which is useful
// for the operator wondering "did this pass because of enforcement or by
// accident?". Best-effort; empty is fine.
func denialDetail(p ProbeID, stderr string) string {
	if stderr == "" {
		return ""
	}
	return "denial observed; stderr tail: " + tailForDetail(stderr)
}

// tailForDetail returns the last ~200 bytes of s for inclusion in a probe
// Detail string. Keeps report lines bounded while preserving the most
// recent (most relevant) diagnostic output.
func tailForDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 200 {
		return s
	}
	return "…" + s[len(s)-200:]
}

// buildProbeChildEnv assembles the probe child's environment: the minimal
// allowlist the child binary needs to run, plus the per-probe context the
// setup planted (canary env name, secret paths, targets). It deliberately
// does NOT copy os.Environ() — baseline hygiene is the P2 enforcement.
func buildProbeChildEnv(setup probeSetup) []string {
	allow := append([]string(nil), defaultChildEnvAllowlist...)
	env := make([]string, 0, len(allow)+len(setup.Env)+2)
	env = append(env,
		probeEnvName+"="+setup.ProbeIDEnv,
		"GOFASTR_PROBE_HOST_UID="+setup.HostUID,
		"GOFASTR_PROBE_HOST_PID="+setup.HostPID,
	)
	env = append(env, setup.Env...)
	seen := make(map[string]bool, len(env))
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok {
			seen[name] = true
		}
	}
	for _, name := range allow {
		if seen[name] {
			continue
		}
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// probeSetup is the per-probe context planted by the runner before
// spawning the probe child. Every field is host-derived; the child reads
// it from env, never invents values.
type probeSetup struct {
	// ProbeIDEnv is the numeric ProbeID as a string (the probeEnvName value).
	ProbeIDEnv string

	// HostUID is the host's uid/gid-equivalent (os.Getuid on Unix,
	// mapped from the process token on Windows) — P1 compares.
	HostUID string

	// HostPID is the probe runner's own pid — P1 tries to signal/proc-read it.
	HostPID string

	// Env is the list of GOFASTR_PROBE_* context vars for this probe
	// (secret paths, net targets, fork count). Sorted for determinism.
	Env []string

	// Cleanup releases host resources the setup opened (the P3 secret fd,
	// the P2 canary env var). Run after the probe child is spawned (the
	// child has already inherited-or-not inherited by then; cleaning up
	// late keeps the suite's host footprint small).
	Cleanup func()
}

// setupProbeContext plants the per-probe host state the child will look
// for: a canary env var + secret file (P2), an open fd to a secret (P3),
// network targets (P4), a host tree path (P5), a fork count (P6). It does
// NOT set up anything the backend is supposed to provide (mounts, cgroup
// limits) — those are the backend.Wrap call's job.
func setupProbeContext(p ProbeID, scratch string) (probeSetup, error) {
	pid := fmt.Sprintf("%d", os.Getpid())
	uid := hostUIDString()
	base := probeSetup{
		ProbeIDEnv: fmt.Sprintf("%d", int(p)),
		HostUID:    uid,
		HostPID:    pid,
	}

	switch p {
	case ProbeDistinctPrincipal, ProbeNoPrivReEscalation:
		// P1/P7 need only the host uid + pid (already in base).
		return base, nil

	case ProbeNoInheritedSecret:
		// Plant a canary env var on the HOST environment (os.Setenv).
		// Baseline hygiene's cmd.Env does NOT copy it through (see
		// buildProbeChildEnv: only the allowlist + probe context). The
		// child reading a non-empty GOFASTR_PROBE_CANARY_ENV value =
		// breach. Also plant a secret FILE outside scratch the sandbox
		// should deny.
		const canary = "GOFASTR_PROBE_CANARY_ENV"
		secretVal := "leaked-host-secret-" + pid
		prev, hadPrev := os.LookupEnv(canary)
		_ = os.Setenv(canary, secretVal)
		secretPath := filepath.Join(scratch, "..", "host-secret-"+pid)
		secretPath = filepath.Clean(secretPath)
		if err := os.WriteFile(secretPath, []byte("file-secret"), 0o600); err != nil {
			_ = os.Unsetenv(canary)
			return probeSetup{}, fmt.Errorf("plant secret file: %w", err)
		}
		cleanup := func() {
			if hadPrev {
				_ = os.Setenv(canary, prev)
			} else {
				_ = os.Unsetenv(canary)
			}
			_ = os.Remove(secretPath)
		}
		base.Env = []string{
			"GOFASTR_PROBE_CANARY_NAME=" + canary,
			"GOFASTR_PROBE_CANARY_VALUE=" + secretVal,
			"GOFASTR_PROBE_SECRET_FILE=" + secretPath,
		}
		base.Cleanup = cleanup
		return base, nil

	case ProbeNoInheritedFD:
		// Open a host fd to a secret pre-spawn. ExtraFiles=nil in the
		// runner means it is NOT inherited; the child enumerating fds > 2
		// finds only the ones the sandbox (or its own runtime) opened.
		// We open the fd in the probe RUNNER process; the child must NOT
		// see it.
		secretPath := filepath.Join(scratch, "fd-secret")
		if err := os.WriteFile(secretPath, []byte("fd-secret"), 0o600); err != nil {
			return probeSetup{}, fmt.Errorf("plant fd secret: %w", err)
		}
		f, err := os.Open(secretPath)
		if err != nil {
			return probeSetup{}, fmt.Errorf("open fd secret: %w", err)
		}
		base.Env = []string{
			"GOFASTR_PROBE_FD_SECRET=" + secretPath,
			"GOFASTR_PROBE_HOST_FD_NUM=" + fmt.Sprintf("%d", f.Fd()),
		}
		base.Cleanup = func() { _ = f.Close() }
		return base, nil

	case ProbeNoNetworkEgress:
		// Loopback, a public IP (1.1.1.1), and the metadata IP. The
		// child dials each; the sandbox denies all three.
		base.Env = []string{
			"GOFASTR_PROBE_NET_TARGETS=127.0.0.1:1,1.1.1.1:53,169.254.169.254:80",
		}
		return base, nil

	case ProbeFilesystemConfinement:
		// Scratch is writable; host paths ($HOME, /etc) are not even
		// readable. The child attempts to read/write outside scratch.
		home, _ := os.UserHomeDir()
		base.Env = []string{
			"GOFASTR_PROBE_SCRATCH=" + scratch,
			"GOFASTR_PROBE_HOME=" + home,
		}
		return base, nil

	case ProbeResourceLimits:
		// The child tries to fork past the cap; the sandbox's cgroup/
		// Job Object caps it. Fork count is well above the default cap
		// (defaultProbePidLimit=64) so an enforced cap fires.
		base.Env = []string{
			"GOFASTR_PROBE_FORK_COUNT=512",
		}
		return base, nil
	}

	return base, fmt.Errorf("unknown probe %d", int(p))
}

// killProcessGroupForCmd is the portable process-tree-kill used when a
// probe exceeds its wall budget. On Unix it signals the negative pgid; on
// Windows it is a best-effort Kill on the recorded handle (a real Job
// Object kill-tree ships with the Windows sandbox backend).
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if pgid := childPgid(cmd); pgid > 0 {
		if err := signalProcessGroup(pgid, 9 /* SIGKILL */); err == nil {
			return nil
		}
	}
	return cmd.Process.Kill()
}

// probeChildMaybeRun is the TestMain dispatcher for the probe-child
// re-exec. When the probeEnvName env var is set, this is a probe child:
// run the probe body for the requested ProbeID, print the result line to
// stdout, and os.Exit — never returning to the test runner. Returns true
// iff the child ran (so TestMain can skip m.Run).
//
// It lives here (not beside processModuleChildMain) because the probe
// child is part of the sandbox deliverable, but it is invoked from the
// package's single TestMain to reuse the existing re-exec dispatch shape.
func probeChildMaybeRun() bool {
	v := os.Getenv(probeEnvName)
	if v == "" {
		return false
	}
	var id ProbeID
	if _, err := fmt.Sscanf(v, "%d", (*int)(&id)); err != nil || id < 1 || int(id) > len(allProbes) {
		fmt.Println(probeOutUnreachable + " bad probe id: " + v)
		os.Exit(0)
	}
	exit := runProbeChildBody(id)
	os.Exit(exit)
	return true
}

// runProbeChildBody is the per-OS probe body dispatcher. It runs the
// forbidden-action attempt for id, prints exactly one result line to
// stdout per the probeOut* protocol, and returns the exit code (0 always —
// the printed line is the truth; the exit code is just for hygiene). The
// Unix and Windows implementations live in processmodule_probe_unix.go /
// _windows.go.
//
// The contract each body MUST honor:
//   - Print exactly one of probeOutPass / probeOutBreach / probeOutUnreachable.
//   - Print before any action that might get the child killed (P6 forks
//     carefully; P4 uses short timeouts so a blocked dial does not run
//     forever without printing).
//   - Never print partial output that could be misparsed (use a single
//     fmt.Println at the end of the body).

// backendName is a nil-safe Name() accessor for the report header.
func backendName(b SandboxBackend) string {
	if b == nil {
		return "none"
	}
	return b.Name()
}

// HostSandboxBackend returns this host's default sandbox backend, or nil
// if no backend is compiled in / available. It is the entry point
// NewSandboxRunner and TestSandboxConformance use to pick up the per-OS
// implementation without callers having to know the build tag.
//
// The per-OS files (processmodule_sandbox_linux.go etc.) provide
// defaultSandboxBackend(); this wrapper exists so a host with no backend
// compiled in (none currently — every build has exactly one) returns nil
// rather than panicking, and so tests can inject a fake.
func HostSandboxBackend() SandboxBackend {
	b := defaultSandboxBackend()
	if b == nil || !b.Available() {
		return nil
	}
	return b
}

// ErrSandboxUnavailable is returned by NewSandboxRunner when the host has
// no probe-passing sandbox backend. It is the constructor-side twin of
// [UntrustedNoSandboxError]: the operator gets one at Register, the
// supervisor wiring gets the other at construction.
var ErrSandboxUnavailable = errors.New("processmodule: no probe-passing sandbox backend available on this host")
