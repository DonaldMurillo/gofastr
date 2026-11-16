package framework

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// ProbeID names one of the sandbox conformance probes P1 to P7.
type ProbeID int

const (
	// ProbeDistinctPrincipal is P1: the child runs as a distinct OS principal.
	ProbeDistinctPrincipal ProbeID = iota + 1

	// ProbeNoInheritedSecret is P2: host env and secret files are invisible.
	ProbeNoInheritedSecret

	// ProbeNoInheritedFD is P3: a host fd opened pre-spawn is not inherited.
	ProbeNoInheritedFD

	// ProbeNoNetworkEgress is P4: every outbound connect fails.
	ProbeNoNetworkEgress

	// ProbeFilesystemConfinement is P5: writes land only under scratch.
	ProbeFilesystemConfinement

	// ProbeResourceLimits is P6: fd, process and memory caps fire.
	ProbeResourceLimits

	// ProbeNoPrivReEscalation is P7: the child cannot regain privilege.
	ProbeNoPrivReEscalation
)

var allProbes = []ProbeID{
	ProbeDistinctPrincipal,
	ProbeNoInheritedSecret,
	ProbeNoInheritedFD,
	ProbeNoNetworkEgress,
	ProbeFilesystemConfinement,
	ProbeResourceLimits,
	ProbeNoPrivReEscalation,
}

// String renders a ProbeID as "P1".."P7".
func (p ProbeID) String() string {
	if p >= 1 && int(p) <= len(allProbes) {
		return fmt.Sprintf("P%d", int(p))
	}
	return fmt.Sprintf("Probe(%d)", int(p))
}

// Title is the human-readable property name.
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
	// ProbeStatusUnknown is a probe that has not been run.
	ProbeStatusUnknown ProbeStatus = iota

	// ProbeStatusPass means the sandbox denied the forbidden action.
	ProbeStatusPass

	// ProbeStatusFail means the forbidden action succeeded.
	ProbeStatusFail

	// ProbeStatusUnreachable means the probe could not be run; it does not
	// conform.
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

	// Detail says why a breach succeeded, why the probe was unreachable, or
	// the denial observed.
	Detail string

	// Reported is true when the probe child printed a result line.
	Reported bool
}

// ConformanceReport is one backend's results for all seven probes.
type ConformanceReport struct {
	Backend string

	// Available reports whether the backend could run on this host.
	Available bool

	MissingReason string

	// Results has one entry per probe, in P1..P7 order.
	Results []ProbeResult
}

// Conforms reports whether the backend was available and every probe
// passed.
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

// Summary renders the report for logs, one line per probe.
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
// applies when wrapping the child exec.
type SandboxOpts struct {
	// ScratchDir is the only directory the child may write.
	ScratchDir string

	// PackageDir holds the module artifact, mounted read-only.
	PackageDir string

	// ReadOnlyDirs are further host paths the child may read.
	ReadOnlyDirs []string

	// PidLimit, MemoryLimitBytes, CpuQuotaPermil (1000 = one CPU) and
	// FdLimit cap the child; 0 is the backend default.
	PidLimit int

	MemoryLimitBytes int64

	CpuQuotaPermil int

	FdLimit int
}

// SandboxBackend is the per-OS confinement wrapper around a child exec.
type SandboxBackend interface {
	// Name is the stable backend identifier ("bwrap", "sandbox-exec", ...).
	Name() string

	// Available reports whether this backend can run on the current host.
	Available() bool

	// MissingReason says why an unavailable backend is unavailable.
	MissingReason() string

	// Wrap rewrites cmd in place to run under this backend's confinement,
	// and errors when it cannot.
	Wrap(cmd *exec.Cmd, opts SandboxOpts) error

	// DeclaredProbes lists the probes this backend aims to enforce here.
	DeclaredProbes() []ProbeID
}

const probeEnvName = "GOFASTR_SANDBOX_PROBE"

const (
	probeOutPass        = "PASS"
	probeOutBreach      = "BREACH"
	probeOutUnreachable = "UNREACHABLE"
)

// RunConformance runs P1 to P7 against b on this host. An unavailable or
// nil b records every probe Unreachable. t may be nil.
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

	dir, err := os.MkdirTemp("", "gofastr-sandbox-probe-*")
	if err == nil {
		//gofastr:allow(worldreadable) traverse-only (0711, no read bit): the sandbox account reaches its own per-probe scratch inside, never the listing or the 0600 secrets
		err = os.Chmod(dir, 0o711)
	}
	if err != nil {
		report.Available = false
		report.MissingReason = "scratch dir: " + err.Error()
		for _, p := range allProbes {
			report.Results = append(report.Results, ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: report.MissingReason})
		}
		return report
	}
	scratch := dir
	defer os.RemoveAll(scratch)

	for _, p := range allProbes {
		select {
		case <-ctx.Done():
			report.Results = append(report.Results, ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "context canceled"})
			continue
		default:
		}
		report.Results = append(report.Results, runOneProbe(ctx, b, p, scratch))
	}
	return report
}

type testingTB interface {
	TempDir() string
	Helper()
}

func runOneProbe(ctx context.Context, b SandboxBackend, p ProbeID, scratch string) ProbeResult {
	exe, err := os.Executable()
	if err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "os.Executable: " + err.Error()}
	}

	probeScratch := filepath.Join(scratch, p.String())
	if err := os.MkdirAll(probeScratch, 0o700); err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "scratch: " + err.Error()}
	}

	setup, setupErr := setupProbeContext(p, probeScratch)
	if setupErr != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "setup: " + setupErr.Error()}
	}
	if setup.Cleanup != nil {
		defer setup.Cleanup()
	}

	cmd := exec.Command(exe)
	cmd.Dir = probeScratch
	cmd.Env = buildProbeChildEnv(setup)
	cmd.Stdin = nil
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	setChildProcessGroup(cmd)

	if err := wrapSandboxSafely(b, cmd, SandboxOpts{
		ScratchDir:       probeScratch,
		PackageDir:       filepath.Dir(exe),
		PidLimit:         defaultProbePidLimit,
		FdLimit:          defaultProbeFdLimit,
		MemoryLimitBytes: defaultProbeMemLimitBytes,
	}); err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "backend.Wrap: " + err.Error()}
	}

	cmd.WaitDelay = 2 * time.Second
	runCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if err := cmd.Start(); err != nil {
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: "start: " + err.Error()}
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var res ProbeResult
	select {
	case <-runCtx.Done():
		_ = killProcessTree(cmd)
		<-waitErr
		res = parseProbeOutput(p, "", true, out.String())
	case err := <-waitErr:
		_ = err
		res = parseProbeOutput(p, out.String(), false, out.String())
	}
	if setup.Check != nil {
		res = setup.Check(res)
	}
	return res
}

var probeTimeout = 15 * time.Second

const (
	defaultProbePidLimit      = 64
	defaultProbeFdLimit       = 32
	defaultProbeMemLimitBytes = 256 * 1024 * 1024
)

func parseProbeOutput(p ProbeID, stdout string, timedOut bool, stderr string) ProbeResult {
	stdout = strings.TrimSpace(stdout)
	first := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, probeOutPass) || strings.HasPrefix(line, probeOutBreach) || strings.HasPrefix(line, probeOutUnreachable) {
			first = line
			break
		}
	}
	if timedOut {
		detail := "probe timed out after " + probeTimeout.String()
		if stderr != "" {
			detail += "; stderr tail: " + tailForDetail(stderr)
		}
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: detail}
	}
	switch {
	case strings.HasPrefix(first, probeOutPass):
		detail := ""
		if stderr != "" {
			detail = "denial observed; stderr tail: " + tailForDetail(stderr)
		}
		return ProbeResult{ID: p, Status: ProbeStatusPass, Detail: detail, Reported: true}
	case strings.HasPrefix(first, probeOutBreach):
		return ProbeResult{ID: p, Status: ProbeStatusFail, Detail: scrubTerminalBytes(strings.TrimSpace(strings.TrimPrefix(first, probeOutBreach))), Reported: true}
	case strings.HasPrefix(first, probeOutUnreachable):
		return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: scrubTerminalBytes(strings.TrimSpace(strings.TrimPrefix(first, probeOutUnreachable))), Reported: true}
	default:
		detail := "child produced no result line"
		if stdout != "" {
			detail = "unrecognized output: " + tailForDetail(stdout)
		}
		if stderr != "" && stdout == "" {
			detail += "; stderr tail: " + tailForDetail(stderr)
		}
		if p == ProbeResourceLimits {
			return ProbeResult{ID: p, Status: ProbeStatusUnreachable, Detail: detail}
		}
		return ProbeResult{ID: p, Status: ProbeStatusFail, Detail: "child killed or crashed before reporting (not a clean denial): " + detail}
	}
}

func tailForDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = "…" + s[len(s)-200:]
	}
	return scrubTerminalBytes(s)
}

func scrubTerminalBytes(s string) string {
	clean := true
	for i := range len(s) {
		if terminalCtrlByte(s[i]) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		if !terminalCtrlByte(s[i]) {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func terminalCtrlByte(c byte) bool {
	if c == '\n' || c == '\t' {
		return false
	}
	return c < 0x20 || c == 0x7f
}

func buildProbeChildEnv(setup probeSetup) []string {
	allow := DefaultChildEnvAllowlist()
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

type probeSetup struct {
	ProbeIDEnv string

	HostUID string

	HostPID string

	Env []string

	Cleanup func()

	Check func(ProbeResult) ProbeResult
}

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
		return base, nil

	case ProbeNoInheritedSecret:
		const canary = "GOFASTR_PROBE_CANARY_ENV"
		secretVal := "leaked-host-secret-" + pid
		prev, hadPrev := os.LookupEnv(canary)
		_ = os.Setenv(canary, secretVal)
		secretPath := filepath.Join(scratch, "..", "host-secret-"+pid)
		secretPath = filepath.Clean(secretPath)
		if err := fileperm.WriteOwnerOnly(secretPath, []byte("file-secret")); err != nil {
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
		secretPath := filepath.Join(scratch, "fd-secret")
		if err := fileperm.WriteOwnerOnly(secretPath, []byte("fd-secret")); err != nil {
			return probeSetup{}, fmt.Errorf("plant fd secret: %w", err)
		}
		f, err := os.Open(secretPath)
		if err != nil {
			return probeSetup{}, fmt.Errorf("open fd secret: %w", err)
		}
		dev, ino, ok := probeFileIdentity(f)
		if !ok {
			_ = f.Close()
			return probeSetup{}, errors.New("planted fd has no device/inode identity on this OS")
		}
		base.Env = []string{
			"GOFASTR_PROBE_FD_SECRET=" + secretPath,
			"GOFASTR_PROBE_HOST_FD_NUM=" + fmt.Sprintf("%d", f.Fd()),
			"GOFASTR_PROBE_FD_DEV=" + strconv.FormatUint(dev, 10),
			"GOFASTR_PROBE_FD_INO=" + strconv.FormatUint(ino, 10),
		}
		base.Cleanup = func() { _ = f.Close() }
		return base, nil

	case ProbeNoNetworkEgress:
		controls, err := openProbeNetControls()
		if err != nil {
			return probeSetup{}, fmt.Errorf("P4 positive control: %w", err)
		}
		addrs := make([]string, 0, len(controls))
		for _, c := range controls {
			addrs = append(addrs, c.ln.Addr().String())
		}
		base.Env = []string{
			"GOFASTR_PROBE_NET_CONTROLS=" + strings.Join(addrs, ","),
			"GOFASTR_PROBE_NET_TARGETS=127.0.0.1:1,1.1.1.1:53,169.254.169.254:80",
		}
		base.Cleanup = func() { closeProbeNetControls(controls) }
		base.Check = func(res ProbeResult) ProbeResult { return probeNetControlsVerdict(controls, res) }
		return base, nil

	case ProbeFilesystemConfinement:
		home, _ := os.UserHomeDir()
		base.Env = []string{
			"GOFASTR_PROBE_SCRATCH=" + scratch,
			"GOFASTR_PROBE_HOME=" + home,
		}
		return base, nil

	case ProbeResourceLimits:
		base.Env = []string{
			"GOFASTR_PROBE_PID_LIMIT=" + strconv.Itoa(defaultProbePidLimit),
			"GOFASTR_PROBE_FD_LIMIT=" + strconv.Itoa(defaultProbeFdLimit),
			"GOFASTR_PROBE_MEM_LIMIT=" + strconv.FormatInt(defaultProbeMemLimitBytes, 10),
		}
		return base, nil
	}

	return base, fmt.Errorf("unknown probe %d", int(p))
}

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

func backendName(b SandboxBackend) string {
	if b == nil {
		return "none"
	}
	return b.Name()
}

// HostSandboxBackend returns this host's sandbox backend, or nil when none
// is available.
func HostSandboxBackend() SandboxBackend {
	b := defaultSandboxBackend()
	if b == nil || !b.Available() {
		return nil
	}
	return b
}

// ErrSandboxUnavailable is returned by NewSandboxRunner when the host has
// no probe-passing sandbox backend.
var ErrSandboxUnavailable = errors.New("processmodule: no probe-passing sandbox backend available on this host")
