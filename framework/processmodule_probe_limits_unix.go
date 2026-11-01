//go:build !windows

package framework

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The P3 (no inherited fd) and P6 (resource limits) probe helpers for
// Unix. Each sub-probe is split into an attempt and a pure
// classification, so the classification can be unit-tested on any Unix
// host without the backend under test.

// ---- P3: the planted fd, by identity ----

// fdIdentity is an open descriptor and the file it names.
type fdIdentity struct {
	FD       int
	Dev, Ino uint64
}

// probeFileIdentity returns the device and inode f names. The probe
// runner records them for the planted P3 fd; an inherited or dup'd copy
// of that fd reports the same pair in the child, whatever its number.
func probeFileIdentity(f *os.File) (dev, ino uint64, ok bool) {
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true
}

// plantedFDIdentity reads the planted fd's identity from the probe env.
func plantedFDIdentity() (fdIdentity, bool) {
	dev, err1 := strconv.ParseUint(os.Getenv("GOFASTR_PROBE_FD_DEV"), 10, 64)
	ino, err2 := strconv.ParseUint(os.Getenv("GOFASTR_PROBE_FD_INO"), 10, 64)
	if err1 != nil || err2 != nil || ino == 0 {
		return fdIdentity{}, false
	}
	return fdIdentity{FD: -1, Dev: dev, Ino: ino}, true
}

// fdScanCandidates lists the fd numbers > 2 worth checking. Linux names
// them in /proc/self/fd; elsewhere it is every number up to the soft
// RLIMIT_NOFILE (capped at 65536), and never fewer than 64 or than the
// host's own number for the planted fd, so a copy kept at that number or
// dup'd low is in range.
func fdScanCandidates(hostFD string) []int {
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		out := make([]int, 0, len(entries))
		for _, e := range entries {
			n, err := strconv.Atoi(e.Name())
			if err == nil && n > 2 {
				out = append(out, n)
			}
		}
		return out
	}
	limit := 64
	if n, err := strconv.Atoi(hostFD); err == nil && n+1 > limit {
		limit = n + 1
	}
	const maxScan = 1 << 16
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err == nil {
		soft := maxScan
		if rl.Cur < maxScan {
			soft = int(rl.Cur)
		}
		limit = max(limit, soft)
	}
	out := make([]int, 0, limit-3)
	for n := 3; n < limit; n++ {
		out = append(out, n)
	}
	return out
}

// openFDIdentities fstats each candidate and returns the open ones. A
// number that is not open (the directory fd ReadDir already closed, a
// gap in the range) is skipped.
func openFDIdentities(candidates []int) []fdIdentity {
	var out []fdIdentity
	for _, fd := range candidates {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) != nil {
			continue
		}
		out = append(out, fdIdentity{FD: fd, Dev: uint64(st.Dev), Ino: uint64(st.Ino)})
	}
	return out
}

// findPlantedFD reports the first open fd naming the planted file. Every
// other fd is the child's own (its runtime, its directory reads) and is
// not a breach.
func findPlantedFD(fds []fdIdentity, want fdIdentity) (int, bool) {
	for _, f := range fds {
		if f.Dev == want.Dev && f.Ino == want.Ino {
			return f.FD, true
		}
	}
	return 0, false
}

// ---- P6: caps that must fire ----

// probeCapThreshold is how far past a requested limit P6 pushes before
// calling the cap missing: twice the limit, and at least eight over it,
// leaving room for what the probe child's own runtime holds.
func probeCapThreshold(limit int) int {
	return max(limit*2, limit+8)
}

// capRun is one P6 sub-probe's attempt: how many units it held at once,
// whether the stop was the cap's own error, and any other error.
type capRun struct {
	held   int
	capped bool
	err    error
}

// runUntilCap acquires units with start, holding every one, until start
// fails or attempts are held. Only an error isCap accepts is the cap
// firing; any other error (a missing binary, a missing /dev/null) is
// recorded as such, never read as a cap. Every unit is released on
// return.
func runUntilCap(attempts int, start func() (release func(), err error), isCap func(error) bool) capRun {
	var releases []func()
	defer func() {
		for _, r := range releases {
			r()
		}
	}()
	for i := range attempts {
		release, err := start()
		if err != nil {
			if isCap(err) {
				return capRun{held: i, capped: true}
			}
			return capRun{held: i, err: err}
		}
		releases = append(releases, release)
	}
	return capRun{held: attempts}
}

// classifyCapRun maps a sub-probe's run to a probe status. A run that
// held probeCapThreshold+1 units saw no cap: BREACH. One stopped by the
// cap's own error at or under the threshold: PASS. Any other stop proves
// nothing: UNREACHABLE.
func classifyCapRun(what string, limit int, r capRun) (ProbeStatus, string) {
	threshold := probeCapThreshold(limit)
	switch {
	case r.err != nil:
		return ProbeStatusUnreachable, fmt.Sprintf("%s: stopped after %d by an error that is not the cap: %v", what, r.held, r.err)
	case r.capped && r.held <= threshold:
		return ProbeStatusPass, fmt.Sprintf("%s capped at %d (limit=%d)", what, r.held, limit)
	default:
		return ProbeStatusFail, fmt.Sprintf("%s: held %d at once (limit=%d, threshold=%d not enforced)", what, r.held, limit, threshold)
	}
}

// isErrno reports whether err is, or wraps, errno.
func isErrno(errno syscall.Errno) func(error) bool {
	return func(err error) bool { return errors.Is(err, errno) }
}

// probeResourceLimitsBody runs the P6 sub-probes in turn (open fds, live
// processes, memory) and returns the first one that does not pass, or a
// pass naming all three. CPU throttling is not probed: it can only be
// read from timing, and a loaded host makes an unthrottled child look
// throttled.
func probeResourceLimitsBody() (ProbeStatus, string) {
	pidLimit, err1 := strconv.Atoi(os.Getenv("GOFASTR_PROBE_PID_LIMIT"))
	fdLimit, err2 := strconv.Atoi(os.Getenv("GOFASTR_PROBE_FD_LIMIT"))
	memLimit, err3 := strconv.ParseInt(os.Getenv("GOFASTR_PROBE_MEM_LIMIT"), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || pidLimit <= 0 || fdLimit <= 0 || memLimit <= 0 {
		return ProbeStatusUnreachable, "missing GOFASTR_PROBE_PID_LIMIT / _FD_LIMIT / _MEM_LIMIT"
	}
	var passed []string

	// Open fds: dup stdout until EMFILE. A dup needs no filesystem, so
	// its only failure on a healthy fd is the per-process cap.
	fds := runUntilCap(probeCapThreshold(fdLimit)+1, func() (func(), error) {
		fd, err := syscall.Dup(1)
		if err != nil {
			return nil, err
		}
		return func() { _ = syscall.Close(fd) }, nil
	}, isErrno(syscall.EMFILE))
	status, detail := classifyCapRun("open fds", fdLimit, fds)
	if status != ProbeStatusPass {
		return status, detail
	}
	passed = append(passed, detail)

	// Live processes: start sleepers and keep every one alive, so a cap
	// on the live count (cgroup pids.max, RLIMIT_NPROC) is reached. Only
	// EAGAIN is the cap; ENOENT for a missing binary is not.
	sleeper := probeSleeperPath()
	if sleeper == "" {
		return ProbeStatusUnreachable, "no sleep binary to hold process slots with"
	}
	pids := runUntilCap(probeCapThreshold(pidLimit)+1, func() (func(), error) {
		pid, err := syscall.ForkExec(sleeper, []string{sleeper, "30"}, &syscall.ProcAttr{Env: []string{}})
		if err != nil {
			return nil, err
		}
		return func() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			var ws syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &ws, 0, nil)
		}, nil
	}, isErrno(syscall.EAGAIN))
	status, detail = classifyCapRun("processes", pidLimit, pids)
	if status != ProbeStatusPass {
		return status, detail
	}
	passed = append(passed, detail)

	status, detail = probeMemoryCap(memLimit)
	if status != ProbeStatusPass {
		return status, detail
	}
	passed = append(passed, detail)
	return ProbeStatusPass, strings.Join(passed, "; ")
}

// probeSleeperPath is a binary that blocks for its argument's seconds.
func probeSleeperPath() string {
	for _, p := range []string{"/bin/sleep", "/usr/bin/sleep"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("sleep"); err == nil {
		return p
	}
	return ""
}

// probeMemHogEnv tells a re-exec'd probe child to be P6's allocating
// child: map and touch the named number of bytes, then print
// probeMemHogDone, or probeMemHogRefused when the kernel refused a
// mapping first, and exit.
const (
	probeMemHogEnv     = "GOFASTR_PROBE_MEM_HOG"
	probeMemHogDone    = "MEMHOG-ALLOCATED"
	probeMemHogRefused = "MEMHOG-REFUSED"
	// probeMemHogChunk is how much the allocating child maps per step.
	probeMemHogChunk = 16 << 20
)

// probeMemoryCap re-execs this binary to allocate twice the memory
// limit and reads how it ended.
func probeMemoryCap(limit int64) (ProbeStatus, string) {
	exe, err := os.Executable()
	if err != nil {
		return ProbeStatusUnreachable, "memory: os.Executable: " + err.Error()
	}
	want := limit * 2
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), probeMemHogEnv+"="+strconv.FormatInt(want, 10))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = time.Second
	runErr := cmd.Run()
	return classifyMemHog(runErr, ctx.Err() != nil, out.String(), want, limit)
}

// classifyMemHog maps the allocating child's end to a probe status. The
// cap fired when the child was SIGKILLed while allocating (a cgroup OOM
// kill) or the Go runtime refused the allocation ("out of memory", an
// rlimit). A child that finished the allocation saw no cap. A timeout
// kill is also SIGKILL and proves nothing, so it is checked first.
func classifyMemHog(runErr error, timedOut bool, output string, want, limit int64) (ProbeStatus, string) {
	if timedOut {
		return ProbeStatusUnreachable, "memory: allocating child timed out"
	}
	if runErr == nil {
		if strings.Contains(output, probeMemHogRefused) {
			return ProbeStatusPass, fmt.Sprintf("memory capped: mapping refused short of %d bytes (limit=%d)", want, limit)
		}
		if strings.Contains(output, probeMemHogDone) {
			return ProbeStatusFail, fmt.Sprintf("memory: allocated and touched %d bytes (limit=%d not enforced)", want, limit)
		}
		return ProbeStatusUnreachable, "memory: allocating child exited without allocating: " + tailForDetail(output)
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL {
			return ProbeStatusPass, fmt.Sprintf("memory capped: allocating child killed short of %d bytes (limit=%d)", want, limit)
		}
		if strings.Contains(output, "out of memory") {
			return ProbeStatusPass, fmt.Sprintf("memory capped: allocation of %d bytes refused (limit=%d)", want, limit)
		}
	}
	return ProbeStatusUnreachable, fmt.Sprintf("memory: allocating child ended with %v: %s", runErr, tailForDetail(output))
}

// runProbeMemHog is the allocating child: it maps v bytes of anonymous
// memory straight from the kernel, a chunk at a time, touching every page
// so it is resident, and reports how far it got. Going around the Go heap
// is the point: a cap that refuses a mapping (RLIMIT_DATA) answers ENOMEM
// here, where the runtime under the same cap can fault in its GC instead
// of failing cleanly, and a crash proves nothing about the cap. A cap
// that kills on use (a cgroup) SIGKILLs the child mid-touch.
func runProbeMemHog(v string) int {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 || n > 1<<34 {
		fmt.Println(probeOutUnreachable + " bad " + probeMemHogEnv + ": " + v)
		return 0
	}
	var held [][]byte
	var got int64
	for got < n {
		size := min(int64(probeMemHogChunk), n-got)
		b, err := syscall.Mmap(-1, 0, int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
		if errors.Is(err, syscall.ENOMEM) {
			fmt.Println(probeMemHogRefused, got)
			return 0
		}
		if err != nil {
			fmt.Println(probeOutUnreachable+" mmap:", err)
			return 0
		}
		for i := 0; i < len(b); i += 4096 {
			b[i] = 1
		}
		held = append(held, b)
		got += size
	}
	fmt.Println(probeMemHogDone, got)
	runtime.KeepAlive(held)
	return 0
}
