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

const (
	bwrapDefaultPidLimit    = 256
	bwrapDefaultFdLimit     = 1024
	bwrapDefaultMemoryBytes = 1 << 30
	bwrapSandboxID          = "65534"
	bwrapSandboxUID         = 65534
)

type bwrapBackend struct {
	bwrapPath   string
	prlimitPath string
	available   bool
	missing     string
}

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

func (b *bwrapBackend) Wrap(cmd *exec.Cmd, opts SandboxOpts) error {
	if b == nil || !b.available {
		return errors.New("bwrap unavailable")
	}
	child := cmd.Path
	if child == "" {
		return errors.New("bwrap: cmd.Path is empty")
	}
	args := append([]string{b.bwrapPath}, bwrapArgs(opts)...)
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

func bwrapArgs(opts SandboxOpts) []string {
	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-user", "--uid", bwrapSandboxID, "--gid", bwrapSandboxID,
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
		"--unshare-cgroup-try",
		"--unshare-net",
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
	args = append(args, "--remount-ro", "/")
	args = append(args, "--cap-drop", "ALL")
	return args
}

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

func (b *bwrapBackend) String() string {
	if b == nil {
		return "bwrap(nil)"
	}
	return fmt.Sprintf("bwrap(%s, prlimit=%s, available=%v)", b.bwrapPath, b.prlimitPath, b.available)
}
