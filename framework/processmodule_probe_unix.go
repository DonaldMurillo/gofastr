//go:build !windows

package framework

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// This file provides the Unix (!windows) implementations of:
//   - runProbeChildBody: the per-probe forbidden-action attempt the
//     conformance suite runs under the candidate backend.
//   - hostUIDString: the host principal string P1 compares against.
//
// The bodies honor the §6 contract: each attempts the forbidden thing,
// then prints exactly one result line per the probeOut* protocol. A clean
// denial (EPERM, EACCES, ENOENT, connection-refused) ⇒ PASS. A success ⇒
// BREACH. An environment where the attempt cannot be initiated at all
// (e.g. no /proc on this Unix) ⇒ UNREACHABLE.

// runProbeChildBody executes the forbidden action for id and prints the
// result line. The returned exit code is always 0, the printed line is
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
		// signal the host, a distinct sandbox principal could not.
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
		// P2 is portable: canary env + secret file both invisible under
		// baseline hygiene (shared body in processmodule_probe_common.go).
		return probeNoInheritedSecretBody(pass, breach)

	case ProbeNoInheritedFD:
		// P3: the host's planted secret fd must not reach the child. The
		// child names it by identity (device + inode, which an inherited
		// or dup'd fd keeps) and ignores every other fd: its own runtime
		// (epoll, eventfd) and the directory it lists hold fds > 2, so
		// counting them made P3 breach on every Linux host.
		hostFD := os.Getenv("GOFASTR_PROBE_HOST_FD_NUM")
		want, ok := plantedFDIdentity()
		if !ok {
			unreachable("no planted fd identity (GOFASTR_PROBE_FD_DEV / GOFASTR_PROBE_FD_INO)")
			return 0
		}
		fds := openFDIdentities(fdScanCandidates(hostFD))
		if fd, found := findPlantedFD(fds, want); found {
			breach(fmt.Sprintf("inherited the host's planted secret fd as fd %d (host fd %s)", fd, hostFD))
			return 0
		}
		pass(fmt.Sprintf("planted secret fd not inherited (%d fds >2 checked, host_fd=%s)", len(fds), hostFD))

	case ProbeNoNetworkEgress:
		// P4 is portable: every dial target must be refused (shared body
		// in processmodule_probe_common.go); Unix breach wording kept here.
		return probeNoNetworkEgressBody(pass, breach, unreachable, "(network egress permitted)")

	case ProbeFilesystemConfinement:
		// P5: host tree / $HOME / secrets unreadable; scratch writable.
		scratch := os.Getenv("GOFASTR_PROBE_SCRATCH")
		home := os.Getenv("GOFASTR_PROBE_HOME")
		// A write outside scratch = breach.
		outOfBounds := filepath.Join(os.TempDir(), "gofastr-probe-escape-"+pidStr())
		//gofastr:allow(fixedtmp) the predictable shared-temp path is the point: this write IS the probe; it must attempt exactly that path to prove the sandbox refuses it
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
		// Sanity: scratch MUST be writable, we don't want a PASS that
		// is "everything is denied including the child's own work".
		if scratch != "" {
			probe := filepath.Join(scratch, ".probe-write")
			if err := fileperm.WriteOwnerOnly(probe, []byte("ok")); err != nil {
				unreachable(fmt.Sprintf("scratch %s not writable: %v", scratch, err))
				return 0
			}
			_ = os.Remove(probe)
		}
		pass("host files unreadable, writes confined to scratch")

	case ProbeResourceLimits:
		// P6: the caps the runner asked the backend for must fire. The
		// memory sub-probe re-execs this binary as the allocating child.
		if v := os.Getenv(probeMemHogEnv); v != "" {
			return runProbeMemHog(v)
		}
		status, detail := probeResourceLimitsBody()
		switch status {
		case ProbeStatusPass:
			pass(detail)
		case ProbeStatusFail:
			breach(detail)
		default:
			unreachable(detail)
		}

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
		// Darwin is not a breach, the setuid failure above is the
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
