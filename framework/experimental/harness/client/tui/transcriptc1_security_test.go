package tui

// Promoted from the harness_transcriptc1 red probe (2026-09-05
// adversarial pass round 4).
// Family: F25 bidi, invisible, and confusable characters
// Property: transcript ingest must strip not only C0/DEL (pinned by
// TestScrollbackStripsTerminalEscapes) but the whole terminal-command and
// display-spoofing class that survives it: 8-bit C1 controls (U+009B CSI,
// U+009D OSC + U+009C ST), bidi overrides (U+202E, U+2066), and zero-width
// characters (U+200B, U+FEFF).
// Surfaces: client/tui/sanitize.go:sanitizeAgentText (C0+DEL only), applied at
// ingestAssistantText / ingestThinkingText / appendCappedMultiline; the same
// bytes reach draw() through every scrollback append in terminal.go.
// Finding: model output, thinking blocks, and tool results carrying "\u009b?1049h"
// (alt-screen flip in C1-8bit terminals), "\u009d0;pwned\u009c" (OSC title rewrite),
// or RLO/zero-width runs are stored verbatim in scrollback and drawn to the
// operator's terminal. The pinned test proved the ESC form is stripped; the
// 8-bit spelling of the SAME sequences and the display-reordering class pass.
// Observed: scrollback rows and terminal output contain every planted rune.
// Severity: medium — same attacker (model output / tool results over a hostile
// repo) as the pinned ESC finding, one encoding step away.
// Fix direction: extend isControlByte (or add a second pass in
// sanitizeAgentText) to strip U+0080–U+009F, U+202A–U+202E, U+2066–U+2069,
// and U+200B–U+200D/U+FEFF/U+2060 at the same ingest boundary.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
)

func hostileRuneIn(s string) (rune, bool) {
	for _, r := range s {
		switch {
		case r >= 0x80 && r <= 0x9f:
			return r, true
		case r == 0x202E || r == 0x2066 || r == 0x202A || r == 0x2069:
			return r, true
		case r == 0x200B || r == 0xFEFF || r == 0x200D || r == 0x2060:
			return r, true
		}
	}
	return 0, false
}

// TestTranscriptScrubC1AndBidi mirrors TestScrollbackStripsTerminalEscapes
// (same surfaces, same ingest paths) for the rune classes that escape its
// C0/DEL sanitizer.
func TestTranscriptScrubC1AndBidi(t *testing.T) {
	shapes := []struct {
		name    string
		payload string
	}{
		{"csi-8bit alt screen", "\u009b?1049h"},
		{"osc-8bit title rewrite", "\u009d0;pwned\u009c"},
		{"rlo display reorder", "safe;\u202E;rm -rf ~"},
		{"zero-width smuggle", "ap\u200Bprove\uFEFF"},
	}
	surfaces := []struct {
		name string
		ev   func(text string) control.Event
	}{
		{"TextDelta text", func(text string) control.Event {
			return control.TextDelta{Text: "say " + text}
		}},
		{"ThinkingDelta block", func(text string) control.Event {
			raw, err := json.Marshal(text)
			if err != nil {
				t.Fatalf("marshal thinking block: %v", err)
			}
			return control.ThinkingDelta{Block: raw}
		}},
		{"ToolResult content", func(text string) control.Event {
			return control.ToolResult{Content: []control.ContentBlock{{Type: "text", Text: text}}}
		}},
	}

	for _, surface := range surfaces {
		// Happy path: clean text must survive ingest unchanged.
		tui, buf := newRenderTestTUI(t)
		render(t, tui, surface.ev("Hello world 100%"))
		tui.draw()
		if !strings.Contains(buf.String(), "Hello world 100%") {
			t.Errorf("%s: clean text did not reach terminal output: %q", surface.name, buf.String())
		}

		for _, shape := range shapes {
			tui, buf := newRenderTestTUI(t)
			render(t, tui, surface.ev(shape.payload))

			for i, ln := range tui.scrollback {
				if strings.HasPrefix(ln, spinnerLineMarker) {
					continue
				}
				if r, bad := hostileRuneIn(ln); bad {
					t.Errorf("SECURITY: %s / %s: scrollback[%d] stores hostile rune %#04x: %q",
						surface.name, shape.name, i, r, ln)
					break
				}
			}
			buf.Reset()
			tui.draw()
			if r, bad := hostileRuneIn(buf.String()); bad {
				t.Errorf("SECURITY: %s / %s: hostile rune %#04x reached terminal output",
					surface.name, shape.name, r)
			}
		}
	}
}
