package framework

import (
	"strings"
	"testing"
)

// Child-process output replayed into ProbeResult.Detail is scrubbed of
// terminal control bytes (CWE-150) — the repo's standard for subprocess
// stderr (codegen's scrubTerminalBytes, documented at
// framework/docs/content/codegen.md: "escape sequences are stripped ...
// Newlines and tabs survive"), applied at tailForDetail so every Detail
// arm inherits it at the single choke point.

// assertProbeDetailNoCtrl fails when a ProbeResult.Detail carries any C0
// control byte other than newline/tab, or DEL — the bytes
// scrubTerminalBytes strips, per codegen.md's "newlines and tabs
// survive".
func assertProbeDetailNoCtrl(t *testing.T, shape, detail string) {
	t.Helper()
	for i := range len(detail) {
		if b := detail[i]; (b < 0x20 && b != '\n' && b != '\t') || b == 0x7f {
			t.Errorf("SECURITY: [probe-ctrl] %s: Detail carries control byte 0x%02X at offset %d: %q — "+
				"child stderr/stdout must not reach operator logs/errors with terminal-control bytes intact "+
				"(repo standard: scrubTerminalBytes)",
				shape, b, i, detail)
			return
		}
	}
}

// TestProbeDetailScrubsControlBytes: a probe PASS whose stderr carries an
// OSC terminal-title sequence, BEL, SOH and DEL (short tail, and a
// >200-byte tail that routes through the truncation arm) must not leak
// any of them into ProbeResult.Detail.
func TestProbeDetailScrubsControlBytes(t *testing.T) {
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
