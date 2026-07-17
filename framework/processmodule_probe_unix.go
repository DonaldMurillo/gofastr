//go:build !windows

package framework

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// This file provides the Unix (!windows) implementations of:
//   - runProbeChildBody — the per-probe forbidden-action attempt the
//     conformance suite runs under the candidate backend.
//   - hostUIDString — the host principal string P1 compares against.
//
// The bodies honor the §6 contract: each attempts the forbidden thing,
// then prints exactly one result line per the probeOut* protocol. A clean
// denial (EPERM, EACCES, ENOENT, connection-refused) ⇒ PASS. A success ⇒
// BREACH. An environment where the attempt cannot be initiated at all
// (e.g. no /proc on this Unix) ⇒ UNREACHABLE.

// runProbeChildBody executes the forbidden action for id and prints the
// result line. The returned exit code is always 0 — the printed line is
// the truth, the exit code is just hygiene (probeChildMaybeRun os.Exit's
// on it regardless).
func runProbeChildBody(id ProbeID) int {
	pass := func(detail string) {
		fmt.Println(strings.TrimSpace(probeOutPass + " " + detail))
	}
	breach := func(detail string) {
		fmt.Println(probeOutBreach + " " + detail)
	}
	unreachable := func(detail string) {
		fmt.Println(probeOutUnreachable + " " + detail)
	}

	switch id {
	case ProbeDistinctPrincipal:
		// P1: child uid ≠ host; cannot signal or /proc-read host pid.
		hostUID := os.Getenv("GOFASTR_PROBE_HOST_UID")
		hostPID := os.Getenv("GOFASTR_PROBE_HOST_PID")
		myUID := fmt.Sprintf("%d", os.Getuid())
		if myUID == hostUID && hostUID != "" {
			breach(fmt.Sprintf("child uid=%s equals host uid=%s (no distinct principal)", myUID, hostUID))
			return 0
		}
		// Signal-0 the host pid: success means we have permission to
		// signal the host — a distinct sandbox principal could not.
		var hp int
		if _, err := fmt.Sscanf(hostPID, "%d", &hp); err == nil && hp > 0 {
			if err := syscall.Kill(hp, 0); err == nil {
				breach(fmt.Sprintf("child can signal host pid %d from uid=%s", hp, myUID))
				return 0
			}
		}
		// Try to read the host's /proc/<pid>/status (Linux path).
		if hostPID != "" {
			if _, err := os.ReadFile("/proc/" + hostPID + "/status"); err == nil {
				breach(fmt.Sprintf("child can /proc-read host pid %s", hostPID))
				return 0
			}
		}
		pass(fmt.Sprintf("child uid=%s isolated from host uid=%s", myUID, hostUID))

	case ProbeNoInheritedSecret:
		// P2: canary env + secret file both invisible under baseline hygiene.
		if name := os.Getenv("GOFASTR_PROBE_CANARY_NAME"); name != "" {
			if got := os.Getenv(name); got != "" {
				breach(fmt.Sprintf("inherited host env %s=%q (baseline hygiene failed)", name, got))
				return 0
			}
		}
		if secret := os.Getenv("GOFASTR_PROBE_SECRET_FILE"); secret != "" {
			if data, err := os.ReadFile(secret); err == nil {
				breach(fmt.Sprintf("read host secret file %s: %q (fs confinement failed)", secret, string(data)))
				return 0
			}
		}
		pass("no canary env, no host secret file visible")

	case ProbeNoInheritedFD:
		// P3: child enumerates fds > 2; an inherited host fd = breach.
		// Linux exposes /proc/self/fd; Darwin lacks it and we fall
		// through to a bounded fstat scan. Go's exec with ExtraFiles
		// nil inherits only 0/1/2, so an enforced baseline surfaces none.
		hostFD := os.Getenv("GOFASTR_PROBE_HOST_FD_NUM")
		var leaked []int
		if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
			for _, e := range entries {
				var n int
				if _, err := fmt.Sscanf(e.Name(), "%d", &n); err != nil {
					continue
				}
				if n > 2 {
					leaked = append(leaked, n)
				}
			}
		} else {
			for n := 3; n < 64; n++ {
				if fdIsOpen(n) {
					leaked = append(leaked, n)
				}
			}
		}
		if len(leaked) > 0 {
			breach(fmt.Sprintf("inherited %d fds >2 (first=%d, host_fd=%s)", len(leaked), leaked[0], hostFD))
			return 0
		}
		pass(fmt.Sprintf("no fds >2 inherited (host_fd=%s)", hostFD))

	case ProbeNoNetworkEgress:
		// P4: every dial fails. Short per-dial timeout so a black-holed
		// route does not hang the probe past its wall budget.
		targets := splitCSV(os.Getenv("GOFASTR_PROBE_NET_TARGETS"))
		if len(targets) == 0 {
			unreachable("no GOFASTR_PROBE_NET_TARGETS")
			return 0
		}
		for _, t := range targets {
			d := net.Dialer{Timeout: 2 * time.Second}
			if c, err := d.Dial("tcp", t); err == nil {
				_ = c.Close()
				breach(fmt.Sprintf("dialed %s (network egress permitted)", t))
				return 0
			}
		}
		pass(fmt.Sprintf("all %d dial targets refused", len(targets)))

	case ProbeFilesystemConfinement:
		// P5: host tree / $HOME / secrets unreadable; scratch writable.
		scratch := os.Getenv("GOFASTR_PROBE_SCRATCH")
		home := os.Getenv("GOFASTR_PROBE_HOME")
		// A write outside scratch = breach.
		outOfBounds := filepath.Join(os.TempDir(), "gofastr-probe-escape-"+pidStr())
		if err := os.WriteFile(outOfBounds, []byte("x"), 0o644); err == nil {
			_ = os.Remove(outOfBounds)
			breach(fmt.Sprintf("wrote outside scratch: %s", outOfBounds))
			return 0
		}
		for _, p := range []string{
			"/etc/passwd",
			filepath.Join(home, ".ssh", "config"),
		} {
			if p == "" {
				continue
			}
			if _, err := os.ReadFile(p); err == nil {
				breach(fmt.Sprintf("read host file %s", p))
				return 0
			}
		}
		// Sanity: scratch MUST be writable — we don't want a PASS that
		// is "everything is denied including the child's own work".
		if scratch != "" {
			probe := filepath.Join(scratch, ".probe-write")
			if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
				unreachable(fmt.Sprintf("scratch %s not writable: %v", scratch, err))
				return 0
			}
			_ = os.Remove(probe)
		}
		pass("host files unreadable, writes confined to scratch")

	case ProbeResourceLimits:
		// P6: fork-bomb capped. The probe runner asked the backend for a
		// cap of defaultProbePidLimit; a backend that enforces it must
		// deny forks beyond ~2× the cap.
		cap := defaultProbePidLimit
		want := 512
		if v := os.Getenv("GOFASTR_PROBE_FORK_COUNT"); v != "" {
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
				want = n
			}
		}
		got := forkBombCount(want)
		threshold := cap * 2
		if threshold < cap+8 {
			threshold = cap + 8
		}
		if got > threshold {
			breach(fmt.Sprintf("forked %d children (cap=%d, threshold=%d not enforced)", got, cap, threshold))
			return 0
		}
		pass(fmt.Sprintf("fork capped at %d children (cap=%d)", got, cap))

	case ProbeNoPrivReEscalation:
		// P7: cannot setuid up, cannot gain new caps.
		if err := syscall.Setuid(0); err == nil {
			breach("setuid(0) succeeded (uid escalation permitted)")
			return 0
		}
		if err := syscall.Setgid(0); err == nil {
			breach("setgid(0) succeeded (gid escalation permitted)")
			return 0
		}
		// Linux: /proc/self/status NoNewPrivs line. Absent field on
		// Darwin is not a breach — the setuid failure above is the
		// enforcement signal there.
		if data, err := os.ReadFile("/proc/self/status"); err == nil {
			if bytes.Contains(data, []byte("NoNewPrivs:\t0")) {
				breach("NoNewPrivs=0 (priv re-escalation path open)")
				return 0
			}
		}
		pass("setuid/setgid escalation refused")

	default:
		unreachable(fmt.Sprintf("unknown probe body %d", int(id)))
	}
	return 0
}

// hostUIDString returns the current process's uid (Unix). Used by the
// probe runner's setup to populate GOFASTR_PROBE_HOST_UID for the P1
// comparison.
func hostUIDString() string {
	return fmt.Sprintf("%d", os.Getuid())
}

// forkBombCount attempts to fork up to want child processes, returning
// the number that actually started. Each child runs /bin/true and exits
// immediately (it exists only to consume a pid slot). Forks happen in
// waves of 16 with a Wait per wave so the live-pid peak stays bounded;
// the point is to observe whether the cumulative fork count is capped,
// not to keep them all alive. A cgroup pids.max caps allocation when old
// pids exit; an RLIMIT_NPROC caps the live set.
func forkBombCount(want int) int {
	const wave = 16
	successes := 0
	for started := 0; started < want; started += wave {
		end := started + wave
		if end > want {
			end = want
		}
		cmds := make([]*exec.Cmd, 0, wave)
		for i := started; i < end; i++ {
			c := exec.Command("/bin/true")
			if err := c.Start(); err != nil {
				// Fork denied — cap likely fired. Stop; the cumulative
				// count is what the caller compares.
				return successes
			}
			cmds = append(cmds, c)
			successes++
		}
		for _, c := range cmds {
			_ = c.Wait()
		}
	}
	return successes
}

// fdIsOpen reports whether fd is currently open in this process. Uses
// fstat (portable across Unix syscall tables) rather than fcntl —
// syscall.FcntlFlock's return arity differs between Linux (uintptr,
// error) and Darwin (error), and fstat's (error) signature is stable.
// Returns false for a closed fd (EBADF).
func fdIsOpen(fd int) bool {
	var st syscall.Stat_t
	return syscall.Fstat(fd, &st) == nil
}

// pidStr is the current pid, formatted, for unique scratch filenames.
func pidStr() string { return fmt.Sprintf("%d", os.Getpid()) }

// splitCSV splits a comma-separated env value, trimming whitespace and
// dropping empties.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
