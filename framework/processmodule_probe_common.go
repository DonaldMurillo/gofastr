package framework

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Portable probe bodies and helpers shared by the build-tagged per-OS
// probe files (processmodule_probe_unix.go /
// processmodule_probe_windows.go). P2 (no inherited secrets) and P4 (no
// network egress) have the same observable-outcome contract on every OS;
// only the P4 breach detail names the OS-specific enforcement mechanism,
// so that wording stays a parameter. This file replaces the former
// byte-duplicated P2/P4 bodies, pidStr, and splitCSV that each tagged
// file carried its own copy of.

// probeNoInheritedSecretBody runs the P2 probe: canary env + secret file
// both invisible under baseline hygiene. Prints exactly one result line
// through pass/breach; the returned exit code is always 0.
func probeNoInheritedSecretBody(pass, breach func(string)) int {
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
	return 0
}

// probeNoNetworkEgressBody runs the P4 probe: every dial target must be
// refused. breachDetail completes the breach message with the platform's
// own wording ("network egress permitted" on Unix, the WFP stub wording
// on Windows). Short per-dial timeout so a black-holed route does not
// hang the probe past its wall budget.
func probeNoNetworkEgressBody(pass, breach, unreachable func(string), breachDetail string) int {
	targets := splitCSV(os.Getenv("GOFASTR_PROBE_NET_TARGETS"))
	if len(targets) == 0 {
		unreachable("no GOFASTR_PROBE_NET_TARGETS")
		return 0
	}
	for _, t := range targets {
		d := net.Dialer{Timeout: 2 * time.Second}
		if c, err := d.Dial("tcp", t); err == nil {
			_ = c.Close()
			breach(fmt.Sprintf("dialed %s %s", t, breachDetail))
			return 0
		}
	}
	pass(fmt.Sprintf("all %d dial targets refused", len(targets)))
	return 0
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
