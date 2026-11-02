//go:build linux

package framework

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The bwrap backend's command line, checked flag by flag. The conformance
// probes (TestSandboxConformance, the processmodule-demo host tests) are
// the proof that the confinement holds on a host with bwrap; this pins
// the shape on every Linux host, bwrap or not, so a dropped flag fails
// here before it fails a probe.

func wrappedBwrap(t *testing.T, opts SandboxOpts) *exec.Cmd {
	t.Helper()
	b := &bwrapBackend{bwrapPath: "/usr/bin/bwrap", prlimitPath: "/usr/bin/prlimit", available: true}
	cmd := exec.Command("/opt/mods/demo", "--flag")
	cmd.Path = "/dev/fd/3"
	if err := b.Wrap(cmd, opts); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	return cmd
}

// hasSeq reports whether want appears in args as a contiguous run.
func hasSeq(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func TestBwrapWrapCommandLine(t *testing.T) {
	scratch := t.TempDir()
	cmd := wrappedBwrap(t, SandboxOpts{ScratchDir: scratch, PackageDir: "/opt/mods", PidLimit: 64, FdLimit: 32, MemoryLimitBytes: 256 << 20})
	args := cmd.Args
	if cmd.Path != "/usr/bin/bwrap" || args[0] != "/usr/bin/bwrap" {
		t.Fatalf("command = %s %v, want bwrap first", cmd.Path, args)
	}
	for _, seq := range [][]string{
		{"--unshare-user", "--uid", "65534", "--gid", "65534"}, // P1
		{"--unshare-pid"},
		{"--unshare-net"}, // P4
		{"--ro-bind", "/opt/mods", "/opt/mods"},
		{"--bind", scratch, scratch, "--chdir", scratch},
		{"--remount-ro", "/"}, // P5
		{"--cap-drop", "ALL"}, // P7
		{"/usr/bin/prlimit", "--nofile=32:32", "--nproc=64:64", "--data=268435456:268435456", "--", "/dev/fd/3", "--flag"}, // P6, then the child
	} {
		if !hasSeq(args, seq...) {
			t.Errorf("bwrap args missing %q:\n%s", seq, strings.Join(args, " "))
		}
	}
	// P5: /etc and /tmp stay out; only the two files the loader needs.
	for i, a := range args {
		if (a == "--ro-bind" || a == "--ro-bind-try" || a == "--bind" || a == "--tmpfs") && i+1 < len(args) {
			src := args[i+1]
			if src == "/etc" || src == "/tmp" || src == "/" {
				t.Errorf("bwrap mounts %s %s: the probe reads /etc/passwd and writes to /tmp", a, src)
			}
		}
	}
	// The root remount comes after every mount point it covers exists.
	if slices.Index(args, "--remount-ro") < slices.Index(args, "--chdir") {
		t.Error("--remount-ro / runs before the scratch bind")
	}
}

// Zero limits take the backend defaults, never "unlimited".
func TestBwrapWrapDefaultLimits(t *testing.T) {
	args := wrappedBwrap(t, SandboxOpts{ScratchDir: t.TempDir()}).Args
	for _, want := range []string{"--nofile=1024:1024", "--nproc=256:256", "--data=1073741824:1073741824"} {
		if !slices.Contains(args, want) {
			t.Errorf("default limits: missing %s in %v", want, args)
		}
	}
}

// A root host starts bwrap as uid 65534 and hands it the scratch dir; a
// non-root host leaves the credentials alone.
func TestBwrapWrapRootRunsAsSandboxAccount(t *testing.T) {
	scratch := t.TempDir()
	cmd := wrappedBwrap(t, SandboxOpts{ScratchDir: scratch})
	cred := cmd.SysProcAttr
	if os.Geteuid() != 0 {
		if cred != nil && cred.Credential != nil {
			t.Fatalf("non-root host: Wrap set credentials %+v", cred.Credential)
		}
		return
	}
	if cred == nil || cred.Credential == nil || cred.Credential.Uid != bwrapSandboxUID || cred.Credential.Gid != bwrapSandboxUID {
		t.Fatalf("root host: bwrap credentials = %+v, want uid/gid %d", cred, bwrapSandboxUID)
	}
	st, err := os.Stat(scratch)
	if err != nil {
		t.Fatal(err)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid != bwrapSandboxUID || sys.Gid != bwrapSandboxUID {
		t.Fatalf("root host: scratch owner = %+v, want %d:%d", st.Sys(), bwrapSandboxUID, bwrapSandboxUID)
	}
}
