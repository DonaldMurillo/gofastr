package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
)

// Pins: a recovered panic value embedding gate-derived text reaches the
// default slog sink scrubbed of C0/DEL/C1/bidi (2026-09-06/07 adversarial
// round 5, phase 2).
// Property: a recovered panic value embedding gate-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi — the scrubControlBytes
// rule pinned for RecoveryFn + Timeout's late-panic log (core/middleware)
// and battery/log's SlogErrorReporter.
// Surfaces: core/mcp/server.go::runCallGate :556-563 (slog.Any("panic",
// rec)) and ::checkServerGate :619-625 (same shape).
// Finding: a host-supplied gate that panics with a value carrying raw
// control bytes (\x1b OSC opener, U+202E bidi override, U+009B CSI) gets
// them logged verbatim by slog.Default(); those bytes paint forged lines
// into the operator's tail (terminal-injection). The recovery itself is
// pinned (TestPanickingGatesAreLoggedNotSwallowed, TestPanickingGateFails-
// ClosedEverywhere) — the scrub of the recovered value is the open gap.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// The scrub probe itself (verbatim slog capture) is internal/scrubtest's
// Sink; see sink.go for why a minimal handler is needed to see raw bytes.

// redGateScrubCheck asserts the scrub contract on a captured panic attr.
func redGateScrubCheck(t *testing.T, got string) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [mcp-gate-panic-log-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: gate-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); every pinned "+
				"recover path (RecoveryFn, Timeout, SlogErrorReporter) scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [mcp-gate-panic-log-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}

func TestCallGatePanicRedScrubbed(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)

	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	s := NewServer()
	if err := s.RegisterTool("ping", "probe tool", map[string]any{"type": "object"},
		func(context.Context, map[string]any) (any, error) { return "ok", nil }); err != nil {
		t.Fatalf("setup broken: register tool: %v", err)
	}
	s.SetCallGate(func(string) error { panic(malicious) })
	if _, err := s.CallTool(context.Background(), "ping", nil); err == nil {
		t.Fatalf("setup broken: panicking call gate did not refuse the call (recovery net missing)")
	}

	got := sink.Get("panic")
	if got == "" {
		t.Fatalf("setup broken: call-gate panic never reached the slog sink (attrs=%v)", sink.Attrs())
	}
	redGateScrubCheck(t, got)
}

func TestServerGatePanicRedScrubbed(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)
	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	s := NewServer()
	s.SetGate(func(context.Context) error { panic(malicious) })
	// Response shape on the listing path is deliberately not asserted
	// (kill-list: no error response demanded there); if the gate's
	// recover net were missing, the panic would crash the test binary.
	_ = s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})

	got := sink.Get("panic")
	if got == "" {
		t.Fatalf("setup broken: server-gate panic never reached the slog sink (attrs=%v)", sink.Attrs())
	}
	redGateScrubCheck(t, got)
}
