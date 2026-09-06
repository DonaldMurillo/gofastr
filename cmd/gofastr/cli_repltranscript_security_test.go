package main

// Pins the unscrubbed transcript print paths in the harness CLI, found by
// the 2026-09-05 red-probe round (round 4); fixed by routing runSingle's
// and streamOneTurn's TextDelta prints through scrubTerminalOutput
// (widened to the core/textsafe set: C0+DEL, 8-bit C1, bidi, zero-width),
// the same class the TUI strips at ingest.
// Family: F25 bidi, invisible, and confusable characters
// Property: model/tool output printed to the operator's terminal by the
// harness CLI transcript paths must pass the same control-byte and
// display-spoof scrub the TUI applies (client/tui sanitizeAgentText):
// ESC/C0/DEL, 8-bit C1, bidi isolators, zero-width.
// Surfaces: cmd/gofastr/harness.go:streamOneTurn (fmt.Print of
// TextDelta text), harness.go:runSingle (same shape; the
// `gofastr harness --prompt` one-shot path), harness.go:runREPL (the
// piped-stdin fallback that drives streamOneTurn per turn).
// Threat: the non-TTY transcript paths printed model TextDelta bytes
// verbatim: an ESC-based OSC 52 (clipboard rewrite), the 8-bit C1
// spellings, and RLO/zero-width all reach the terminal. TextDelta text
// is attacker-influenced the same way the TUI's is (model output, tool
// results over a hostile repo, provider error bodies).

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// TextDelta and asserts the terminal never sees the control class.
func TestREPLTranscriptScrubTerminal(t *testing.T) {
	sess := ids.NewSessionID()

	envelope := func(e control.Event) control.EventEnvelope {
		t.Helper()
		env, err := control.EncodeEvent(1, e, sess, ids.NewClientID(), time.Now())
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		return env
	}

	shapes := []struct {
		name    string
		payload string
	}{
		{"osc52 clipboard via ESC", "\x1b]52;c;aG90IGNsaXBib2FyZA==\x07"},
		{"csi-8bit alt screen", "\u009b?1049h"},
		{"rlo display reorder", "safe;\u202E;rm -rf ~"},
		{"zero-width smuggle", "ap\u200Bprove\uFEFF"},
	}

	// Happy path: plain text must still stream through unchanged.
	clean := captureStdout(t, func() {
		ch := make(chan control.EventEnvelope, 2)
		ch <- envelope(control.TextDelta{Text: "Hello world 100%"})
		ch <- envelope(control.TurnEnded{})
		close(ch)
		streamOneTurn(ch)
	})
	if !strings.Contains(clean, "Hello world 100%") {
		t.Fatalf("fixture check: clean transcript text was dropped: %q", clean)
	}

	for _, shape := range shapes {
		out := captureStdout(t, func() {
			ch := make(chan control.EventEnvelope, 2)
			ch <- envelope(control.TextDelta{Text: shape.payload})
			ch <- envelope(control.TurnEnded{})
			close(ch)
			streamOneTurn(ch)
		})
		for _, bad := range []rune{'\x1b', '\u009b', '\u009d', '\u202E', '\u200B', '\uFEFF'} {
			if strings.ContainsRune(out, bad) {
				t.Errorf("SECURITY: [injection] %s: hostile rune %#04x reached the terminal via streamOneTurn (no scrub on this path)", shape.name, bad)
			}
		}
	}
}
