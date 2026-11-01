//go:build !windows

package framework

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The sandbox conformance gate admits a backend only when P1–P7 pass, so
// a probe that passes without testing anything admits a backend that
// enforces nothing. These tests pin the P3 and P6 classifications
// (2026-10-31 bug hunt, S72/S73) on any Unix host, without a backend.

// plantProbeFD writes and opens a planted P3 secret and sets the env the
// P3 body reads. The caller closes the returned file.
func plantProbeFD(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fd-secret")
	if err := os.WriteFile(path, []byte("fd-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	dev, ino, ok := probeFileIdentity(f)
	if !ok {
		t.Fatal("no identity for the planted fd")
	}
	t.Setenv("GOFASTR_PROBE_HOST_FD_NUM", strconv.Itoa(int(f.Fd())))
	t.Setenv("GOFASTR_PROBE_FD_DEV", strconv.FormatUint(dev, 10))
	t.Setenv("GOFASTR_PROBE_FD_INO", strconv.FormatUint(ino, 10))
	return f
}

// P3 counted every fd > 2 in the child as inherited, and a Go child
// always holds some of its own (epoll, eventfd, the directory it lists),
// so P3 breached on every Linux host. Only the planted fd counts.
func TestP3BodyMatchesOnlyPlantedFD(t *testing.T) {
	f := plantProbeFD(t)
	// This test process holds many fds > 2 of its own, the way a probe
	// child does. With the planted one open it must breach...
	if line := runProbeBodyLine(t, ProbeNoInheritedFD); !strings.HasPrefix(line, probeOutBreach) {
		t.Fatalf("planted fd open: body printed %q, want BREACH", line)
	}
	// ...and with it closed, every other fd is the process's own.
	_ = f.Close()
	if line := runProbeBodyLine(t, ProbeNoInheritedFD); !strings.HasPrefix(line, probeOutPass) {
		t.Fatalf("planted fd closed, own fds open: body printed %q, want PASS", line)
	}
}

func TestP3FindsPlantedFDByIdentity(t *testing.T) {
	want := fdIdentity{FD: -1, Dev: 7, Ino: 42}
	own := []fdIdentity{{FD: 3, Dev: 0, Ino: 9}, {FD: 4, Dev: 0, Ino: 10}, {FD: 5, Dev: 7, Ino: 41}}
	if fd, ok := findPlantedFD(own, want); ok {
		t.Fatalf("matched fd %d, which names another file", fd)
	}
	dup := append(own, fdIdentity{FD: 9, Dev: 7, Ino: 42})
	if fd, ok := findPlantedFD(dup, want); !ok || fd != 9 {
		t.Fatalf("findPlantedFD = %d, %v; want 9, true", fd, ok)
	}
}

func TestP3NoIdentityIsUnreachable(t *testing.T) {
	t.Setenv("GOFASTR_PROBE_FD_DEV", "")
	t.Setenv("GOFASTR_PROBE_FD_INO", "")
	if line := runProbeBodyLine(t, ProbeNoInheritedFD); !strings.HasPrefix(line, probeOutUnreachable) {
		t.Fatalf("no planted identity: body printed %q, want UNREACHABLE", line)
	}
}

// forkBombCount stopped at the first Start error of any kind, so ENOENT
// for a binary the sandbox does not mount read as "the cap fired" and P6
// passed having tested nothing.
func TestCapRunOtherErrorIsNotCap(t *testing.T) {
	enoent := &os.PathError{Op: "fork/exec", Path: "/bin/true", Err: syscall.ENOENT}
	r := runUntilCap(100, func() (func(), error) { return nil, enoent }, isErrno(syscall.EAGAIN))
	if st, detail := classifyCapRun("processes", 64, r); st != ProbeStatusUnreachable {
		t.Fatalf("ENOENT classified %s (%s), want unreachable", st, detail)
	}
	n := 0
	eagain := &os.PathError{Op: "fork/exec", Path: "/bin/sleep", Err: syscall.EAGAIN}
	r = runUntilCap(200, func() (func(), error) {
		if n == 60 {
			return nil, eagain
		}
		n++
		return func() {}, nil
	}, isErrno(syscall.EAGAIN))
	if st, detail := classifyCapRun("processes", 64, r); st != ProbeStatusPass {
		t.Fatalf("EAGAIN at 60 of limit 64 classified %s (%s), want pass", st, detail)
	}
	r = runUntilCap(probeCapThreshold(64)+1, func() (func(), error) { return func() {}, nil }, isErrno(syscall.EAGAIN))
	if st, detail := classifyCapRun("processes", 64, r); st != ProbeStatusFail {
		t.Fatalf("no cap at all classified %s (%s), want fail", st, detail)
	}
}

// The waves of 16 with a Wait each kept at most 16 children alive, so a
// correct pids.max=64 never fired. Every unit is held until the run ends.
func TestCapRunHoldsEveryUnit(t *testing.T) {
	live, peak := 0, 0
	r := runUntilCap(50, func() (func(), error) {
		live++
		peak = max(peak, live)
		return func() { live-- }, nil
	}, isErrno(syscall.EAGAIN))
	if r.held != 50 || peak != 50 {
		t.Fatalf("held %d with peak %d live, want 50 and 50", r.held, peak)
	}
	if live != 0 {
		t.Fatalf("%d units still held after the run", live)
	}
}

func TestClassifyMemHog(t *testing.T) {
	killed := exitErrorFrom(t, "kill -9 $$")
	failed := exitErrorFrom(t, "exit 2")
	for _, tc := range []struct {
		name     string
		err      error
		timedOut bool
		out      string
		want     ProbeStatus
	}{
		{"allocated", nil, false, probeMemHogDone + " 8", ProbeStatusFail},
		{"oom killed", killed, false, "", ProbeStatusPass},
		{"runtime refused", failed, false, "fatal error: runtime: out of memory", ProbeStatusPass},
		{"mapping refused", nil, false, probeMemHogRefused + " 4", ProbeStatusPass},
		// A timeout kill is SIGKILL too, and proves nothing.
		{"timeout kill", killed, true, "", ProbeStatusUnreachable},
		{"other exit", failed, false, "no such file", ProbeStatusUnreachable},
		{"silent", nil, false, "", ProbeStatusUnreachable},
	} {
		if got, detail := classifyMemHog(tc.err, tc.timedOut, tc.out, 8, 4); got != tc.want {
			t.Errorf("%s: %s (%s), want %s", tc.name, got, detail, tc.want)
		}
	}
}

// The allocating child really allocates: unconfined, twice the limit is
// reached and reported, so the memory sub-probe breaches.
// TestMemHogRefusedUnderDataCap is its confined twin.
func TestMemHogUnconfinedBreaches(t *testing.T) {
	t.Setenv(probeEnvName, strconv.Itoa(int(ProbeResourceLimits)))
	if st, detail := probeMemoryCap(4 << 20); st != ProbeStatusFail {
		t.Fatalf("unconfined memory sub-probe: %s (%s), want fail", st, detail)
	}
}

func exitErrorFrom(t *testing.T, script string) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", script).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("sh -c %q: %v, want an exit error", script, err)
	}
	return fmt.Errorf("probe child: %w", err)
}

// Under a data cap the allocating child is refused a mapping and says so
// cleanly, rather than the Go runtime faulting in its GC: the mmap path
// is what makes RLIMIT_DATA provable. Runs the child under prlimit; skips
// where prlimit is not installed.
func TestMemHogRefusedUnderDataCap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_DATA via prlimit is the linux backend's cap")
	}
	if raceEnabled {
		t.Skip("-race build: ThreadSanitizer cannot start under a data cap")
	}
	prlimit, err := exec.LookPath("prlimit")
	if err != nil {
		t.Skip("prlimit not installed")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const limit = 256 << 20
	cmd := exec.Command(prlimit, fmt.Sprintf("--data=%d:%d", limit, limit), "--", exe)
	cmd.Env = append(os.Environ(), probeEnvName+"="+strconv.Itoa(int(ProbeResourceLimits)), probeMemHogEnv+"="+strconv.Itoa(2*limit))
	out, runErr := cmd.CombinedOutput()
	if st, detail := classifyMemHog(runErr, false, string(out), 2*limit, limit); st != ProbeStatusPass {
		t.Fatalf("capped allocating child: %s (%s), want pass\n%s", st, detail, out)
	}
}
