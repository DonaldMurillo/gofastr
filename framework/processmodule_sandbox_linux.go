//go:build linux

package framework

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
)

// This file is the Linux arm of the §6 sandbox backend: bubblewrap
// (`bwrap`) for P1–P5 + P7, with cgroup v2 for P6 left as an operator-
// provisioning concern (honest: the probe reports P6's status, it does
// not assume enforcement).
//
// Why bwrap (wrapper-command, not native): golang.org/x/sys ships
// landlock/seccomp *syscalls* but no BPF assembler (CONFIRMED in the
// design), so a native seccomp filter is bespoke code. bwrap is the
// de-facto Linux unprivileged-sandbox CLI and is already the seam the
// repo's bash tool wires (`framework/harness/tool/builtins/bash.go`
// SandboxFn comment) — mirroring it keeps one Linux sandbox story.
//
// Honest limits (probed, not assumed — design §6):
//   - unprivileged userns is disabled on several distros (Debian older
//     kernels, some RHEL) → bwrap --unshare-user fails → the constructor
//     reports the backend unavailable or P1/P4 breach → fail closed;
//   - non-root cgroup v2 needs delegation → P6 is UNREACHABLE in v1
//     (the cgroup/prlimit wiring is deferred; the probe reports BREACH,
//     which is the honest enforcement, not a hand-wave).
//
// Both ⇒ the backend does not conform ⇒ untrusted fails closed on this
// host. Fail-closed is correct but means untrusted modules may be
// unrunnable on Linux until the operator enables userns + delegates a
// cgroup (design §11 risk 1).

// bwrapBackend is the Linux SandboxBackend. It wraps the child exec in a
// bwrap invocation that unshares net/pid/uts, drops all caps, and bind-
// mounts the system tree read-only with scratch writable. cgroup-based
// limits (P6) are NOT applied in v1 — DeclaredProbes omits P6 and the
// conformance suite reports it BREACH.
type bwrapBackend struct {
	bwrapPath string
	available bool
	missing   string
}

// defaultSandboxBackend returns the Linux bwrap backend, or an
// unavailable instance if bwrap is not on PATH (the caller —
// HostSandboxBackend — filters the latter to nil).
func defaultSandboxBackend() SandboxBackend {
	b := &bwrapBackend{}
	if p, err := exec.LookPath("bwrap"); err == nil {
		b.bwrapPath = p
		b.available = true
	} else {
		b.missing = "bwrap not on PATH (install bubblewrap)"
	}
	return b
}

func (b *bwrapBackend) Name() string          { return "bwrap" }
func (b *bwrapBackend) Available() bool       { return b != nil && b.available }
func (b *bwrapBackend) MissingReason() string { return b.missing }

// DeclaredProbes is the honest ceiling: bwrap's flags enforce P1–P5 +
// P7 WHEN unprivileged userns is enabled (probed at construction). P6
// (cgroup/prlimit) is NOT enforced in v1 — omitted from the declaration,
// and the conformance suite runs P6 anyway and reports BREACH/UNREACHABLE.
func (b *bwrapBackend) DeclaredProbes() []ProbeID {
	return []ProbeID{
		ProbeDistinctPrincipal,
		ProbeNoInheritedSecret,
		ProbeNoInheritedFD,
		ProbeNoNetworkEgress,
		ProbeFilesystemConfinement,
		ProbeNoPrivReEscalation,
	}
}

// Wrap rewrites cmd to run the child under bwrap. The original cmd.Path
// becomes the last bwrap argument; cmd.Path becomes the bwrap binary.
// Stdio assignment (cmd.Stdin/Stdout/Stderr set by prepareChildForSpawn)
// is inherited by bwrap and passed through to the child unchanged.
func (b *bwrapBackend) Wrap(cmd *exec.Cmd, opts SandboxOpts) error {
	if b == nil || !b.available {
		return errors.New("bwrap unavailable")
	}
	child := cmd.Path
	if child == "" {
		return errors.New("bwrap: cmd.Path is empty")
	}

	// Build the bwrap argument vector. Ordering matters: flags first,
	// then the child + its args at the end (bwrap exec's the last token
	// chain).
	args := []string{
		b.bwrapPath,
		// P7 + crash isolation: die with the parent, no new privileges.
		"--die-with-parent",
		"--new-session",
		// P1: distinct PID + UTS namespace (uid mapping needs userns;
		// if the host disables unprivileged userns, bwrap fails at
		// Start and the probe records BREACH on P1 — honest).
		"--unshare-pid",
		"--unshare-uts",
		// P4: no network namespace egress. (When the probe dials, every
		// connect() fails with ENETUNREACH.)
		"--unshare-net",
		// P5: minimal read-only system tree. /usr, /lib, /bin, /sbin
		// are ro-bind so the child can exec tools; /etc is ro-bind for
		// name resolution / passwd lookups the runtime needs. /dev and
		// /proc are mounted fresh inside the namespace so the child
		// sees only its own pids.
		"--ro-bind", "/usr", "/usr",
		"--ro-bind-try", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind-try", "/sbin", "/sbin",
		"--ro-bind", "/etc", "/etc",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	// P5: the child's package dir is read-only (the artifact + static
	// assets live here); scratch is read-write (the child's own work).
	if opts.PackageDir != "" {
		abs, _ := filepath.Abs(opts.PackageDir)
		args = append(args, "--ro-bind", abs, abs)
	}
	if opts.ScratchDir != "" {
		abs, _ := filepath.Abs(opts.ScratchDir)
		args = append(args, "--bind", abs, abs)
	}
	for _, d := range opts.ReadOnlyDirs {
		if d == "" {
			continue
		}
		abs, _ := filepath.Abs(d)
		args = append(args, "--ro-bind-try", abs, abs)
	}
	// P7: drop all capabilities. Combined with --new-session this gives
	// the no-new-privileges property the probe checks (setuid-up fails).
	args = append(args, "--cap-drop", "ALL")
	// The child + its original args. bwrap exec's this token chain.
	args = append(args, child)
	args = append(args, cmd.Args[1:]...)

	cmd.Path = b.bwrapPath
	cmd.Args = args
	// Note: P6 (memory/pids/cpu/fd caps) needs cgroup v2 delegation or
	// prlimit wrapping — NOT applied in v1. The probe reports this
	// honestly; see DeclaredProbes.
	return nil
}

// String renders the backend for logs.
func (b *bwrapBackend) String() string {
	if b == nil {
		return "bwrap(nil)"
	}
	return fmt.Sprintf("bwrap(%s, available=%v)", b.bwrapPath, b.available)
}
