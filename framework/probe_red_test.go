//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass round 3 (tests-only; no fix applied).
// Property: child-process output replayed to an operator is scrubbed of
// terminal control bytes (CWE-150) — the repo's own standard for subprocess
// stderr: codegen's extension_command.go scrubTerminalBytes, mirrored by
// cmd/gofastr/generate.go scrubTerminalOutput, documented at
// framework/docs/content/codegen.md:99-101 ("escape sequences are stripped
// ... Newlines and tabs survive"). The probe path replays module-runner child
// output the same way and must meet the same bar.
// Surfaces: framework/processmodule_probe.go tailForDetail (:591-597), via
// parseProbeOutput's stderr arms (timeout detail :541-545, PASS denialDetail
// :549/:581-586, no-sentinel detail :563-573) → ProbeResult.Detail →
// ConformanceReport.Summary (:231-233), which NewSandboxRunner embeds whole
// into ErrSandboxUnavailable (processmodule_sandbox.go:89-90) and the test
// harness prints — operator-facing error strings and logs.
// Finding: tailForDetail does TrimSpace + a 200-byte cap ONLY, so ESC/BEL/DEL
// and every other C0 byte in the probe child's stderr reaches Detail verbatim.
// An OSC sequence in wrapper/child output (e.g. the terminal-title
// \x1b]0;x\x07) can rewrite the operator's window title or, via OSC 52, the
// clipboard from inside an error string.
// Severity: operator-local parity gap — the shipped probe child is the host
// binary plus the sandbox wrapper (bwrap/sandbox-exec), so reaching a live
// terminal needs attacker-influenced bytes in wrapper stderr (module
// names/paths appear there on failure); this is the repo-standard scrub
// extended to a sibling surface, not a demonstrated remote exploit.
// Fix direction: scrub in tailForDetail with the scrubTerminalBytes contract
// — strip C0 except \n and \t, plus DEL — so every Detail arm inherits it at
// the single choke point.

package framework

import (
	"strings"
	"testing"
)

// assertProbeDetailNoCtrl fails when a ProbeResult.Detail carries any C0
// control byte other than newline/tab, or DEL — the bytes scrubTerminalBytes
// strips, per codegen.md's "newlines and tabs survive".
func assertProbeDetailNoCtrl(t *testing.T, shape, detail string) {
	t.Helper()
	for i := range len(detail) {
		if b := detail[i]; (b < 0x20 && b != '\n' && b != '\t') || b == 0x7f {
			t.Errorf("SECURITY: [probe-ctrl] %s: Detail carries control byte 0x%02X at offset %d: %q — "+
				"tailForDetail (processmodule_probe.go:591) trims and caps but never scrubs, so child "+
				"stderr reaches operator logs/errors with terminal-control bytes intact (repo standard: "+
				"codegen scrubTerminalBytes, codegen.md:99-101)",
				shape, b, i, detail)
			return
		}
	}
}

// TestProbeRedScrubsStderrControlBytes: a probe PASS whose stderr carries an
// OSC terminal-title sequence, BEL, SOH and DEL (short tail, and a >200-byte
// tail that routes through the truncation arm) must not leak any of them into
// ProbeResult.Detail.
func TestProbeRedScrubsStderrControlBytes(t *testing.T) {
	stdout := probeOutPass + " uid isolated"

	// Short stderr: tailForDetail returns it whole after TrimSpace.
	short := "\x1b]0;pwn\x07 denial observed: EPERM \x01 mid \x7f end"
	got := parseProbeOutput(ProbeDistinctPrincipal, stdout, false, short)
	if got.Status != ProbeStatusPass {
		t.Fatalf("setup: expected Pass, got %v (%q)", got.Status, got.Detail)
	}
	assertProbeDetailNoCtrl(t, "short stderr tail", got.Detail)

	// Long stderr: the escape bytes sit at the END so they land inside the
	// last-200-bytes window the truncation arm keeps.
	long := strings.Repeat("x", 260) + "\n\x1b]0;pwn\x07 EACCES \x7f"
	got = parseProbeOutput(ProbeDistinctPrincipal, stdout, false, long)
	if got.Status != ProbeStatusPass {
		t.Fatalf("setup: expected Pass, got %v (%q)", got.Status, got.Detail)
	}
	assertProbeDetailNoCtrl(t, "long stderr tail (truncated arm)", got.Detail)
}
