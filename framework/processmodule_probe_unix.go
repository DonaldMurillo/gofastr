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
		hostUID := os.Getenv("GOFASTR_PROBE_HOST_UID")
		hostPID := os.Getenv("GOFASTR_PROBE_HOST_PID")
		myUID := fmt.Sprintf("%d", os.Getuid())
		if myUID == hostUID && hostUID != "" {
			breach(fmt.Sprintf("child uid=%s equals host uid=%s (no distinct principal)", myUID, hostUID))
			return 0
		}
		var hp int
		if _, err := fmt.Sscanf(hostPID, "%d", &hp); err == nil && hp > 0 {
			if err := syscall.Kill(hp, 0); err == nil {
				breach(fmt.Sprintf("child can signal host pid %d from uid=%s", hp, myUID))
				return 0
			}
		}
		if hostPID != "" {
			if _, err := os.ReadFile("/proc/" + hostPID + "/status"); err == nil {
				breach(fmt.Sprintf("child can /proc-read host pid %s", hostPID))
				return 0
			}
		}
		pass(fmt.Sprintf("child uid=%s isolated from host uid=%s", myUID, hostUID))

	case ProbeNoInheritedSecret:
		return probeNoInheritedSecretBody(pass, breach)

	case ProbeNoInheritedFD:
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
		return probeNoNetworkEgressBody(pass, breach, unreachable, "(network egress permitted)")

	case ProbeFilesystemConfinement:
		scratch := os.Getenv("GOFASTR_PROBE_SCRATCH")
		home := os.Getenv("GOFASTR_PROBE_HOME")
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
		if err := syscall.Setuid(0); err == nil {
			breach("setuid(0) succeeded (uid escalation permitted)")
			return 0
		}
		if err := syscall.Setgid(0); err == nil {
			breach("setgid(0) succeeded (gid escalation permitted)")
			return 0
		}
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

func hostUIDString() string {
	return fmt.Sprintf("%d", os.Getuid())
}
