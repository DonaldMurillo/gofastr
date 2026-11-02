//go:build linux

package framework

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// This file is the Linux arm of the §6 sandbox backend: bubblewrap
// (`bwrap`) for P1–P5 + P7 and util-linux `prlimit` for P6, both run as
// wrapper commands: bwrap builds the namespaces and mounts, then execs
// prlimit inside them, which sets the rlimits and execs the child.
//
// Why bwrap (wrapper-command, not native): golang.org/x/sys ships
// landlock/seccomp *syscalls* but no BPF assembler (CONFIRMED in the
// design), so a native seccomp filter is bespoke code. bwrap is the
// de-facto Linux unprivileged-sandbox CLI and is already the seam the
// repo's bash tool wires (`framework/experimental/harness/tool/builtins/bash.go`
// SandboxFn comment), mirroring it keeps one Linux sandbox story.
//
// How each probe is met (the probes, not this comment, are the proof):
//   - P1: a user namespace maps the child to uid/gid 65534. Outside the
//     namespace the kernel still charges the child to the host user, so
//     the separation is the namespaces': the child sees only its own pid
//     namespace and only the mounts below, so it cannot signal or read
//     the host's processes or files. It is not a separate host account.
//   - P5: the sandbox root is bwrap's tmpfs, remounted read-only once the
//     mounts are in place; the system tree is read-only and only scratch
//     is writable. /etc is NOT mounted, only the two files a dynamically
//     linked child needs (ld.so.cache, localtime): /etc/passwd and the
//     rest stay out. There is no /tmp: a module writes to its working
//     directory, which is scratch.
//   - P6: RLIMIT_NOFILE, RLIMIT_NPROC and RLIMIT_DATA, set by prlimit
//     after the user namespace exists, so NPROC counts the sandbox's own
//     processes and threads only. RLIMIT_DATA caps the child's heap: the
//     Go runtime fails "out of memory" past it. CPU is not capped (that
//     needs a delegated cgroup v2; the probe does not check CPU).
//   - P7: all capabilities dropped and uid 0 unmapped, so setuid(0)
//     fails; bwrap sets no_new_privs.
//   - A root host starts bwrap as host uid 65534 (runBwrapAsSandboxUser):
//     the kernel never applies RLIMIT_NPROC to host root, so P6 needs it.
//
// Honest limits (probed, not assumed, design §6): unprivileged user
// namespaces are disabled on some distros and restricted by AppArmor on
// others. bwrap then fails at spawn, every probe child dies unreported,
// the backend does not conform, and untrusted modules fail closed on that
// host (design §11 risk 1).

// bwrap backend defaults for a module spawn whose SandboxOpts leave a
// limit at zero. Generous enough for an ordinary Go module (NPROC counts
// threads), tight enough to stop a fork bomb or a runaway heap.
const (
	bwrapDefaultPidLimit    = 256
	bwrapDefaultFdLimit     = 1024
	bwrapDefaultMemoryBytes = 1 << 30
	// bwrapSandboxID is the uid and gid the child runs as inside its user
	// namespace: the conventional "nobody", never the host's own. A root
	// host also runs bwrap itself as this id (see runBwrapAsSandboxUser).
	bwrapSandboxID  = "65534"
	bwrapSandboxUID = 65534
)

// bwrapBackend is the Linux SandboxBackend: bwrap for the namespaces and
// mounts, prlimit for the resource caps.
type bwrapBackend struct {
	bwrapPath   string
	prlimitPath string
	available   bool
	missing     string
}

// defaultSandboxBackend returns the Linux bwrap backend, or an
// unavailable instance if bwrap or prlimit is not on PATH (the caller,
// HostSandboxBackend, filters the latter to nil).
func defaultSandboxBackend() SandboxBackend {
	b := &bwrapBackend{}
	bw, err := exec.LookPath("bwrap")
	if err != nil {
		b.missing = "bwrap not on PATH (install bubblewrap)"
		return b
	}
	pl, err := exec.LookPath("prlimit")
	if err != nil {
		b.missing = "prlimit not on PATH (install util-linux)"
		return b
	}
	b.bwrapPath, b.prlimitPath, b.available = bw, pl, true
	return b
}

func (b *bwrapBackend) Name() string          { return "bwrap" }
func (b *bwrapBackend) Available() bool       { return b != nil && b.available }
func (b *bwrapBackend) MissingReason() string { return b.missing }

// DeclaredProbes is every probe: each is met by the mechanism named in the
// file comment, and the conformance run still checks each one on the host.
func (b *bwrapBackend) DeclaredProbes() []ProbeID {
	return []ProbeID{
		ProbeDistinctPrincipal,
		ProbeNoInheritedSecret,
		ProbeNoInheritedFD,
		ProbeNoNetworkEgress,
		ProbeFilesystemConfinement,
		ProbeResourceLimits,
		ProbeNoPrivReEscalation,
	}
}

// Wrap rewrites cmd to run the child under bwrap and prlimit. The
// original cmd.Path becomes the last command; cmd.Path becomes the bwrap
// binary. Stdio and ExtraFiles (the verified artifact fd the runner execs
// through /dev/fd/3) are inherited by bwrap and prlimit and reach the
// child unchanged.
func (b *bwrapBackend) Wrap(cmd *exec.Cmd, opts SandboxOpts) error {
	if b == nil || !b.available {
		return errors.New("bwrap unavailable")
	}
	child := cmd.Path
	if child == "" {
		return errors.New("bwrap: cmd.Path is empty")
	}
	args := append([]string{b.bwrapPath}, bwrapArgs(opts)...)
	// P6: prlimit runs inside the namespaces, sets the caps, execs the child.
	args = append(args, b.prlimitPath)
	args = append(args, prlimitArgs(opts)...)
	args = append(args, "--", child)
	args = append(args, cmd.Args[1:]...)

	if err := runBwrapAsSandboxUser(cmd, opts.ScratchDir); err != nil {
		return err
	}
	cmd.Path = b.bwrapPath
	cmd.Args = args
	return nil
}

// runBwrapAsSandboxUser starts bwrap as host uid/gid 65534 when the host
// runs as root. The kernel exempts every process whose real uid is root
// from RLIMIT_NPROC, user namespace or not, and bwrap run by root maps
// the sandbox's 65534 back to host root, so P6's process cap would never
// fire. Run as 65534 instead, the child is a separate host account as
// well as a separate namespace uid. Scratch is the one host path it
// writes, so this spawn's scratch directory is handed to that account;
// every other mount source must already be readable by it (a package dir
// only root can read fails the spawn, closed). A non-root host is left as
// it is: unprivileged bwrap already maps the child away from it.
func runBwrapAsSandboxUser(cmd *exec.Cmd, scratch string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if scratch != "" {
		if err := os.Lchown(scratch, bwrapSandboxUID, bwrapSandboxUID); err != nil {
			return fmt.Errorf("bwrap: hand scratch to uid %d: %w", bwrapSandboxUID, err)
		}
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: bwrapSandboxUID, Gid: bwrapSandboxUID}
	return nil
}

// bwrapArgs is the bwrap flag vector, everything before the command.
// Ordering matters: bwrap applies the mount operations in order, so the
// read-only remount of the root comes after every mount point under it
// exists.
func bwrapArgs(opts SandboxOpts) []string {
	args := []string{
		// Crash isolation: die with the parent, own session (no TIOCSTI
		// into the host's terminal).
		"--die-with-parent",
		"--new-session",
		// P1: own user namespace, mapped to nobody.
		"--unshare-user", "--uid", bwrapSandboxID, "--gid", bwrapSandboxID,
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
		"--unshare-cgroup-try",
		// P4: own network namespace with no interfaces but loopback.
		"--unshare-net",
		// P5: read-only system tree, enough to exec and dynamically link.
		"--ro-bind", "/usr", "/usr",
		"--ro-bind-try", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--ro-bind-try", "/lib32", "/lib32",
		"--ro-bind-try", "/bin", "/bin",
		"--ro-bind-try", "/sbin", "/sbin",
		"--ro-bind-try", "/etc/ld.so.cache", "/etc/ld.so.cache",
		"--ro-bind-try", "/etc/localtime", "/etc/localtime",
		"--proc", "/proc",
		"--dev", "/dev",
	}
	// P5: the child's package dir is read-only (the artifact + static
	// assets live here); scratch is read-write (the child's own work).
	if opts.PackageDir != "" {
		abs, _ := filepath.Abs(opts.PackageDir)
		args = append(args, "--ro-bind", abs, abs)
	}
	for _, d := range opts.ReadOnlyDirs {
		if d == "" {
			continue
		}
		abs, _ := filepath.Abs(d)
		args = append(args, "--ro-bind-try", abs, abs)
	}
	if opts.ScratchDir != "" {
		abs, _ := filepath.Abs(opts.ScratchDir)
		args = append(args, "--bind", abs, abs, "--chdir", abs)
	}
	// P5: nothing outside scratch is writable, the tmpfs root included.
	args = append(args, "--remount-ro", "/")
	// P7: drop every capability inside the namespace.
	args = append(args, "--cap-drop", "ALL")
	return args
}

// prlimitArgs sets the P6 caps, soft and hard alike so the child cannot
// raise its own soft limit back up.
func prlimitArgs(opts SandboxOpts) []string {
	pids := opts.PidLimit
	if pids <= 0 {
		pids = bwrapDefaultPidLimit
	}
	fds := opts.FdLimit
	if fds <= 0 {
		fds = bwrapDefaultFdLimit
	}
	mem := opts.MemoryLimitBytes
	if mem <= 0 {
		mem = bwrapDefaultMemoryBytes
	}
	return []string{
		fmt.Sprintf("--nofile=%d:%d", fds, fds),
		fmt.Sprintf("--nproc=%d:%d", pids, pids),
		fmt.Sprintf("--data=%d:%d", mem, mem),
	}
}

// String renders the backend for logs.
func (b *bwrapBackend) String() string {
	if b == nil {
		return "bwrap(nil)"
	}
	return fmt.Sprintf("bwrap(%s, prlimit=%s, available=%v)", b.bwrapPath, b.prlimitPath, b.available)
}
