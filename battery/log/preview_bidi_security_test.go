package log

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"testing"
)

// Pins: no invisible/bidi/C1 codepoint reaches a terminal sink in live form (round 5, fixed).
// Property: no invisible/bidi/C1 codepoint reaches a terminal sink in live
// form — pinned for the console renderer by invisible_security_test.go
// (round 4, family F25); the fanout fallback preview bypasses that scrub.
// slog's JSON encoder escapes C0 controls as \u text but leaves C1/bidi
// runes raw, and Handle writes those preview bytes straight to os.Stderr.
// Surfaces: battery/log/log.go::fanoutHandler.Handle :472-476 — the
// throttled 256-byte raw entry preview written to os.Stderr when a sink
// fails.
// Finding: attr values carrying U+202E (RLO) or U+009B (CSI) render live
// into the stderr preview line: 8-bit terminals execute 0x9B as an escape
// introducer and RLO reorders the rendered line — terminal injection on
// the operator channel.
// Fix direction: run the preview through the same core/textsafe scrub (or
// JSON-quote it) the console renderer applies before Fprintf.

func TestFanoutRedStderrPreviewScrubbed(t *testing.T) {
	sink := failingSink{err: errors.New("disk full")}
	h := newFanoutHandler([]Sink{sink}, nil)
	logger := slog.New(h)

	origStderr := os.Stderr
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("setup broken: pipe: %v", perr)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	logger.Info("order.created", "email", "alice\u202Eevil@example.com", "note", "x\u009B31m")

	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	got := buf.Bytes()

	// Positive controls: the fallback itself stays pinned — the sink error
	// and the preview marker must still reach stderr.
	if !bytes.Contains(got, []byte("disk full")) || !bytes.Contains(got, []byte("preview")) {
		t.Fatalf("setup broken: stderr did not capture the sink-error fallback preview; got=%q", got)
	}
	if bytes.ContainsRune(got, '\u202E') {
		t.Errorf("SECURITY: [log-preview-bidi] the stderr fallback preview carries a live U+202E RIGHT-TO-LEFT OVERRIDE: %q — the fanout preview bypasses the console renderer's invisible/bidi scrub, so the operator's terminal reorders the line", got)
	}
	if bytes.ContainsRune(got, '\u009B') {
		t.Errorf("SECURITY: [log-preview-bidi] the stderr fallback preview carries a live U+009B CSI (8-bit escape introducer): %q — slog's JSON encoder leaves C1 runes raw and Handle writes the preview bytes straight to os.Stderr, so the operator's terminal executes them as terminal controls", got)
	}
}
