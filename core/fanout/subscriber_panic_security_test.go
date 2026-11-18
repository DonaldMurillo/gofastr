package fanout

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
)

// Pins: a recovered panic value embedding subscriber-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi (2026-09-06/07
// adversarial round 5, phase 2).
// Property: a recovered panic value embedding subscriber-derived text
// reaches the default slog sink scrubbed of C0/DEL/C1/bidi — the
// scrubControlBytes rule pinned for RecoveryFn + Timeout's late-panic
// log (core/middleware) and battery/log's SlogErrorReporter.
// Surfaces: core/fanout/subscriber_queue.go::deliver :21-24 —
// slog.Any("panic", r) with the raw recovered value.
// Finding: a subscriber callback that panics with a value carrying raw
// control bytes (\x1b OSC opener, U+202E bidi override, U+009B CSI) gets
// them logged verbatim by slog.Default(); those bytes paint forged lines
// into the operator's tail (terminal-injection) on the framework-owned
// delivery goroutine, where no HTTP recover net ever sees them.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// The scrub probe itself (verbatim slog capture) is internal/scrubtest's
// Sink; see sink.go for why a minimal handler is needed to see raw bytes.

func TestFanoutRedSubscriberPanicScrub(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)

	// The panic value a subscriber echoing hostile input would build:
	// visible text plus a raw OSC title-set, an RLO bidi override, and
	// an 8-bit CSI introducer.
	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	deliver(func([]byte) { panic(malicious) }, []byte("probe"))

	got := sink.Get("panic")
	if got == "" {
		t.Fatalf("setup broken: subscriber panic never reached the slog sink (attrs=%v)", sink.Attrs())
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [fanout-panic-log-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: subscriber-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection) on the framework-owned "+
				"delivery goroutine; every pinned recover path (RecoveryFn, Timeout, SlogErrorReporter) "+
				"scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [fanout-panic-log-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
