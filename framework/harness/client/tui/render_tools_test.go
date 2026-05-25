package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestRenderToolCallStartedShowsToolName: ToolCallStarted lands in
// scrollback with the ⚙ marker so the user can see which tool fired.
func TestRenderToolCallStartedShowsToolName(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"ls"}`), Mutating: true,
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "Bash") {
		t.Errorf("ToolCallStarted not surfaced: %v", tui.scrollback)
	}
	if !strings.Contains(tui.scrollback[0], "●") {
		t.Errorf("missing ● marker: %q", tui.scrollback[0])
	}
}

// TestRenderToolResultShowsResult: ToolResult lands with a ✓ or ✗
// marker.
func TestRenderToolResultShowsResult(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	ok, _ := control.EncodeEvent(1, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: "file contents"}},
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(ok)
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "⎿") {
		t.Errorf("success result missing ⎿ corner: %v", tui.scrollback)
	}
	tui = &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	fail, _ := control.EncodeEvent(1, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: "boom"}},
		IsError: true,
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(fail)
	if !strings.Contains(tui.scrollback[0], "⎿") || !strings.Contains(tui.scrollback[0], "✗") {
		t.Errorf("error result missing ⎿ corner or ✗: %v", tui.scrollback)
	}
}

// TestRenderThinkingDeltaShowsBubble: ThinkingDelta surfaces under
// the 🤔 marker so the user can see the model's reasoning.
func TestRenderThinkingDeltaShowsBubble(t *testing.T) {
	// Wire-format invariant: Block holds a json.RawMessage that must
	// be valid JSON. The openai-compatible adapter JSON-quotes the
	// reasoning string before publishing, so we do the same here.
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	quoted, _ := json.Marshal("Let me think about this...")
	env, _ := control.EncodeEvent(1, control.ThinkingDelta{
		Block: quoted,
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "…") {
		t.Errorf("thinking not surfaced: %v", tui.scrollback)
	}
	if !strings.Contains(tui.scrollback[0], "Let me think") {
		t.Errorf("thinking content missing: %q", tui.scrollback[0])
	}
}

// TestThinkingCoalesces: consecutive ThinkingDelta events without an
// intervening event continue the same 🤔 line.
func TestThinkingCoalesces(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	for _, chunk := range []string{"part1 ", "part2 ", "part3"} {
		quoted, _ := json.Marshal(chunk)
		env, _ := control.EncodeEvent(1, control.ThinkingDelta{Block: quoted},
			tui.Session, ids.NewClientID(), time.Now())
		tui.renderEvent(env)
	}
	if len(tui.scrollback) != 1 {
		t.Errorf("thinking didn't coalesce, got %d lines: %v", len(tui.scrollback), tui.scrollback)
	}
	if !strings.Contains(tui.scrollback[0], "part1 part2 part3") {
		t.Errorf("thinking chunks not joined: %q", tui.scrollback[0])
	}
}

// TestContinuationLinesAreFlushLeft: model markdown like
// "## Heading" must land at column 0, not indented by a TUI prefix
// (the bug from the second screenshot).
func TestContinuationLinesAreFlushLeft(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.TextDelta{
		Text: "intro\n## Section\n- bullet item",
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	if len(tui.scrollback) < 3 {
		t.Fatalf("not split into multiple lines: %v", tui.scrollback)
	}
	// Find the "## Section" line — it must NOT start with spaces.
	for _, line := range tui.scrollback {
		if strings.Contains(line, "## Section") {
			if strings.HasPrefix(line, " ") {
				t.Errorf("heading was indented (bug from screenshot): %q", line)
			}
		}
	}
}
