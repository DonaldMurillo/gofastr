//go:build windows

package framework

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file provides the Windows implementations of:
//   - runProbeChildBody: the per-probe forbidden-action attempt the
//     conformance suite runs under the candidate backend.
//   - hostUIDString: the host principal string P1 compares against.
//
// Windows reaches the §6 denials through AppContainer/restricted tokens
// (P1/P5/P7) + Job Objects (P6) + per-SID WFP firewall rules (P4). The v1
// Windows backend (processmodule_sandbox_windows.go) is a documented
// stub: it reports every probe UNREACHABLE, which causes untrusted
// modules to fail-closed on Windows until the operator provisions a real
// backend. The probe bodies below mirror the portable probes (P2/P4/P5)
// so that WHEN a real Windows backend lands, the same observable-outcome
// contract exercises it; the bodies that have no portable Windows shape
// (P1 uid/SID, P3 handle enumeration, P6 fork, P7 token-mediation) report
// UNREACHABLE here rather than fake a result.

// runProbeChildBody executes the forbidden action for id and prints the
// result line. Returns 0 always; the printed line is the truth.
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
		// P1 on Windows compares the process token SID against the
		// host's. The stub backend does not mint an AppContainer token,
		// so the child runs under the host's token, they match.
		// Honest outcome: BREACH (no distinct principal), which
		// contributes to the Windows fail-closed until a real backend
		// provisions a restricted token.
		hostSID := os.Getenv("GOFASTR_PROBE_HOST_UID")
		breach(fmt.Sprintf("Windows stub: child token == host token (%s); AppContainer/restricted-token backend not wired", hostSID))

	case ProbeNoInheritedSecret:
		// P2 is portable: baseline hygiene's env scrub + filesystem
		// confinement work the same as on Unix (shared body in
		// processmodule_probe_common.go).
		return probeNoInheritedSecretBody(pass, breach)

	case ProbeNoInheritedFD:
		// P3 on Windows: handle inheritance under Go's exec is
		// controlled by the syscall.SysProcAttr.InheritedHandles
		// field; the stub does not enumerate inherited handles
		// portably. Mark UNREACHABLE, a real Windows backend
		// would assert bInheritHandles=FALSE here.
		unreachable("Windows fd-inheritance probe needs InheritedHandles plumbing (stub)")

	case ProbeNoNetworkEgress:
		// P4 is portable: dial each target (shared body in
		// processmodule_probe_common.go). The Windows stub does not
		// install per-SID WFP rules, so egress succeeds, honest BREACH;
		// the WFP wording lives here.
		return probeNoNetworkEgressBody(pass, breach, unreachable, "(no WFP egress rule; stub permits egress)")

	case ProbeFilesystemConfinement:
		// P5: write/read outside scratch. Windows equivalent of the
		// Unix host-tree probe: try to read a system file the child
		// should not see, and try to write outside scratch.
		scratch := os.Getenv("GOFASTR_PROBE_SCRATCH")
		outOfBounds := filepath.Join(os.Getenv("PROGRAMDATA"), "gofastr-probe-escape-"+pidStr())
		//gofastr:allow(fixedtmp) the predictable shared-temp path is the point: this write IS the probe; it must attempt exactly that path to prove the sandbox refuses it
		if err := os.WriteFile(outOfBounds, []byte("x"), 0o644); err == nil {
			_ = os.Remove(outOfBounds)
			breach(fmt.Sprintf("wrote outside scratch: %s", outOfBounds))
			return 0
		}
		// Sanity: scratch writable.
		if scratch != "" {
			probe := filepath.Join(scratch, ".probe-write")
			if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
				unreachable(fmt.Sprintf("scratch %s not writable: %v", scratch, err))
				return 0
			}
			_ = os.Remove(probe)
		}
		pass("writes confined to scratch (host read paths not probed on stub)")

	case ProbeResourceLimits:
		// P6 on Windows uses a Job Object (kill-tree + memory/pids/CPU
		// limits). The stub does not assign one; fork is not a Windows
		// primitive. Mark UNREACHABLE, a real backend would assign the
		// child to a Job Object with per-limit fields and the probe
		// would observe denial.
		unreachable("Windows Job-Object limits not wired (stub)")

	case ProbeNoPrivReEscalation:
		// P7 on Windows: token-mediated (AdjustTokenPrivileges,
		// create-restricted-token). The stub does not mint a restricted
		// token, so re-escalation is host-default. Honest UNREACHABLE.
		unreachable("Windows restricted-token backend not wired (stub)")

	default:
		unreachable(fmt.Sprintf("unknown probe body %d", int(id)))
	}
	return 0
}

// hostUIDString returns a Windows-side identifier for the host principal.
// The v1 stub maps this to the username; a real backend compares SIDs.
func hostUIDString() string {
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "unknown-windows-user"
}
