package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Property: terminal-bound output must not carry content-borne terminal
// control sequences.
//
// Text ingested into scrollback is attacker-influenced bytes: model
// TextDelta text, thinking blocks, tool results (read/grep/glob output
// over files that live in a cloned repo), provider error messages
// (which embed raw upstream HTTP bodies), and TurnStarted content from
// peer clients. draw() writes scrollback lines to t.out with only ANSI
// *added* around them, so any ESC/C0 byte that survives ingest executes
// in the operator's terminal: OSC 52 rewrites the clipboard, OSC 0 the
// window title, CSI ?1049h flips the alternate screen, CSI private
// modes rebind keys/mouse, and bare C0 bytes overwrite the current row.
//
// The surfaces below are every renderEvent path that carries event text
// into scrollback. ToolCallStarted args are exempt: args travel as
// json.RawMessage, so raw control bytes cannot survive the envelope
// codec — only the escaped \u001b spelling can, and that renders as
// inert text.
//
// The fix belongs in one sanitizer applied where scrollback lines are
// appended (ingestAssistantText, ingestThinkingText,
// appendCappedMultiline, and the raw appends in renderEvent), not in
// draw(): scrollback is storage, and storing an ESC byte there hands
// every future renderer the same bug.
func TestScrollbackStripsTerminalEscapes(t *testing.T) {
	shapes := []struct {
		name    string
		payload string
	}{
		{"osc52 clipboard write", "\x1b]52;c;aG90IGNsaXBib2FyZA==\x07"},
		{"osc title rewrite", "\x1b]0;pwned\x07"},
		{"csi alt screen flip", "\x1b[?1049h"},
		{"csi private mode", "\x1b[?1000;1006h"},
		{"c0 row overwrite", "ok\rEVIL\x07\x08"},
	}
	surfaces := []struct {
		name string
		ev   func(text string) control.Event
	}{
		{"TextDelta text", func(text string) control.Event {
			return control.TextDelta{Text: "say " + text}
		}},
		{"ThinkingDelta block", func(text string) control.Event {
			// Block arrives as a JSON string literal; the codec keeps
			// it escaped, ingestThinkingText unquotes to raw bytes.
			raw, err := json.Marshal(text)
			if err != nil {
				t.Fatalf("marshal thinking block: %v", err)
			}
			return control.ThinkingDelta{Block: raw}
		}},
		{"ToolResult content", func(text string) control.Event {
			return control.ToolResult{Content: []control.ContentBlock{{Type: "text", Text: text}}}
		}},
		{"Error message", func(text string) control.Event {
			return control.Error{Reason: "upstream", Message: "HTTP 500: " + text}
		}},
		{"TurnStarted user text", func(text string) control.Event {
			return control.TurnStarted{Turn: 1, Content: []control.ContentBlock{{Type: "text", Text: text}}}
		}},
	}

	for _, surface := range surfaces {
		// Happy path first: clean text must survive ingest and reach
		// the terminal unchanged, so the fix cannot strip by blunt
		// truncation after an ESC.
		tui, buf := newRenderTestTUI(t)
		render(t, tui, surface.ev("Hello world 100%"))
		tui.draw()
		if !strings.Contains(buf.String(), "Hello world 100%") {
			t.Errorf("%s: clean text did not reach terminal output: %q", surface.name, buf.String())
		}

		for _, shape := range shapes {
			tui, buf := newRenderTestTUI(t)
			render(t, tui, surface.ev(shape.payload))

			// Storage layer: scrollback lines must never store a
			// control byte (tab excepted). The spinner sentinel line
			// is the TUI's own and is glyph-substituted at draw time.
			for i, ln := range tui.scrollback {
				if strings.HasPrefix(ln, spinnerLineMarker) {
					continue
				}
				if c, bad := ctrlByte(ln); bad {
					t.Errorf("SECURITY: %s / %s: scrollback[%d] stores control byte %#02x: %q",
						surface.name, shape.name, i, c, ln)
					break
				}
			}

			// Terminal layer: the payload bytes must not reach t.out
			// verbatim (draw adds its own ANSI, so only the exact
			// payload is asserted here).
			buf.Reset()
			tui.draw()
			if strings.Contains(buf.String(), shape.payload) {
				t.Errorf("SECURITY: %s / %s: payload reached terminal output verbatim: %q",
					surface.name, shape.name, shape.payload)
			}
		}
	}
}

// render feeds one event through the envelope codec, mirroring the
// real delivery path (EncodeEvent on the bus, DecodeEvent in the TUI).
// String fields round-trip raw control bytes: the codec escapes them
// on the wire and restores them at decode.
func render(t *testing.T, tui *TUI, e control.Event) {
	t.Helper()
	env, err := control.EncodeEvent(1, e, tui.Session, ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	tui.renderEvent(env)
}

// ctrlByte reports the first C0 control byte (tab excepted) or DEL in
// s, the byte class a terminal interprets rather than prints.
func ctrlByte(s string) (byte, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return c, true
		}
	}
	return 0, false
}

// TestProgressLinesStripTerminalEscapes extends the terminal-control
// property to the two renderEvent paths the test above does not reach:
//
//   - ToolCallProgress.Partial is a plain string straight off the
//     EventSink (tool.go: "surfaces render them"), and the codec
//     round-trips raw control bytes in string fields — verified: a
//     Partial of "out\x1b]52;c;…\x07more" decodes with the ESC and
//     BEL intact. terminal.go:329 renders it with
//     appendMultiline("  ⎣ ", truncate(v.Partial, 400)) and NO
//     sanitizeAgentText, so a streaming tool (bash output, webfetch
//     progress) that emits OSC/CSI bytes writes them into scrollback
//     and out to the operator's terminal.
//   - PermissionRequested args are pinned as the boundary case: they
//     render through summarizeArgs, which prints the raw JSON bytes,
//     so control bytes arrive only in their inert \u001b spelling.
//     This must STAY true — the permission prompt is where a human
//     approves an invocation, and a live escape sequence there can
//     visually rewrite the command being approved.
func TestProgressLinesStripTerminalEscapes(t *testing.T) {
	shapes := []struct {
		name    string
		payload string
	}{
		{"osc52 clipboard write", "building \x1b]52;c;aG90IGNsaXBib2FyZA==\x07 done"},
		{"csi alt screen flip", "\x1b[?1049h"},
		{"csi private mode", "step 1 \x1b[?1000;1006h step 2"},
		{"c0 row overwrite", "ok\rEVIL\x07\x08"},
	}
	surfaces := []struct {
		name string
		ev   func(text string) control.Event
	}{
		{"ToolCallProgress partial", func(text string) control.Event {
			return control.ToolCallProgress{CallID: ids.NewCallID(), Partial: text}
		}},
		{"PermissionRequested args", func(text string) control.Event {
			// Encoded the way the wire carries it: a control byte in a
			// JSON string is escaped (\u001b), never raw, and decodes
			// back to the byte the terminal would interpret.
			args, err := json.Marshal(map[string]string{"cmd": "echo " + text})
			if err != nil {
				t.Fatal(err)
			}
			return control.PermissionRequested{
				CallID: ids.NewCallID(),
				Tool:   "Bash",
				Args:   json.RawMessage(args),
			}
		}},
	}

	for _, surface := range surfaces {
		// Happy path: clean progress/approval text must survive ingest.
		tui, buf := newRenderTestTUI(t)
		render(t, tui, surface.ev("compiling 42%"))
		tui.draw()
		if !strings.Contains(buf.String(), "compiling 42%") {
			t.Errorf("%s: clean text did not reach terminal output: %q", surface.name, buf.String())
		}

		for _, shape := range shapes {
			tui, buf := newRenderTestTUI(t)
			render(t, tui, surface.ev(shape.payload))
			for i, ln := range tui.scrollback {
				if strings.HasPrefix(ln, spinnerLineMarker) {
					continue
				}
				if c, bad := ctrlByte(ln); bad {
					t.Errorf("SECURITY: %s / %s: scrollback[%d] stores control byte %#02x: %q",
						surface.name, shape.name, i, c, ln)
					break
				}
			}
			buf.Reset()
			tui.draw()
			if strings.Contains(buf.String(), shape.payload) {
				t.Errorf("SECURITY: %s / %s: payload reached terminal output verbatim: %q",
					surface.name, shape.name, shape.payload)
			}
		}
	}
}
