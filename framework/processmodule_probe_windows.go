//go:build windows

package framework

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		hostSID := os.Getenv("GOFASTR_PROBE_HOST_UID")
		breach(fmt.Sprintf("Windows stub: child token == host token (%s); AppContainer/restricted-token backend not wired", hostSID))

	case ProbeNoInheritedSecret:
		return probeNoInheritedSecretBody(pass, breach)

	case ProbeNoInheritedFD:
		unreachable("Windows fd-inheritance probe needs InheritedHandles plumbing (stub)")

	case ProbeNoNetworkEgress:
		return probeNoNetworkEgressBody(pass, breach, unreachable, "(no WFP egress rule; stub permits egress)")

	case ProbeFilesystemConfinement:
		scratch := os.Getenv("GOFASTR_PROBE_SCRATCH")
		outOfBounds := filepath.Join(os.Getenv("PROGRAMDATA"), "gofastr-probe-escape-"+pidStr())
		//gofastr:allow(fixedtmp) the predictable shared-temp path is the point: this write IS the probe; it must attempt exactly that path to prove the sandbox refuses it
		if err := os.WriteFile(outOfBounds, []byte("x"), 0o644); err == nil {
			_ = os.Remove(outOfBounds)
			breach(fmt.Sprintf("wrote outside scratch: %s", outOfBounds))
			return 0
		}
		if scratch != "" {
			probe := filepath.Join(scratch, ".probe-write")
			if err := fileperm.WriteOwnerOnly(probe, []byte("ok")); err != nil {
				unreachable(fmt.Sprintf("scratch %s not writable: %v", scratch, err))
				return 0
			}
			_ = os.Remove(probe)
		}
		pass("writes confined to scratch (host read paths not probed on stub)")

	case ProbeResourceLimits:
		unreachable("Windows Job-Object limits not wired (stub)")

	case ProbeNoPrivReEscalation:
		unreachable("Windows restricted-token backend not wired (stub)")

	default:
		unreachable(fmt.Sprintf("unknown probe body %d", int(id)))
	}
	return 0
}

func hostUIDString() string {
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "unknown-windows-user"
}

func probeFileIdentity(*os.File) (dev, ino uint64, ok bool) { return 0, 0, false }
