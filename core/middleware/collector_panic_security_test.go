package middleware

import (
	"io"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
)

// Pins: a recovered panic value embedding collector-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi (2026-09-06/07
// adversarial round 5, phase 2).
// Property: a recovered panic value embedding collector-derived text
// reaches the default slog sink scrubbed of C0/DEL/C1/bidi — the
// textsafe.ScrubControlBytes rule pinned for RecoveryFn + Timeout's late-panic
// log (this package) and battery/log's SlogErrorReporter.
// Surface: core/middleware/metrics.go::runCollectorSafely :250-256 —
// "error", textsafe.Truncate(fmt.Sprint(r), maxRecoveryPanicLen): truncate only,
// no scrub (the sibling RecoveryFn path scrubs AND truncates).
// Finding: a third-party CollectorFunc that panics with a value carrying
// raw control bytes (\x1b OSC opener, U+202E bidi override, U+009B CSI)
// gets them logged verbatim by slog.Default(); those bytes paint forged
// lines into the operator's tail (terminal-injection). The isolation
// itself is pinned (TestMetricsCollectorPanicIsolated) — the scrub of
// the recovered value is the open gap, and it sits in the very package
// that owns the scrub rule.
// Fix direction: run the truncated value through textsafe.ScrubControlBytes (the
// same rule the package's own RecoveryFn applies).
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// The scrub probe itself (verbatim slog capture) is internal/scrubtest's
// Sink, shared with this package's other sink call sites.

func TestCollectorPanicRedScrubbed(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)

	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	out := runCollectorSafely("red_probe", func(io.Writer) { panic(malicious) })
	if out != nil {
		t.Fatalf("setup broken: panicking collector contributed output (isolation contract)")
	}

	// This surface logs the recovered value under the "error" key.
	got := sink.Get("error")
	if got == "" {
		t.Fatalf("setup broken: collector panic never reached the slog sink (attrs=%v)", sink.Attrs())
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [metrics-collector-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in error attr %q. Attack: collector-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); this package's own "+
				"RecoveryFn and Timeout paths scrub before logging — runCollectorSafely only truncates.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [metrics-collector-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
