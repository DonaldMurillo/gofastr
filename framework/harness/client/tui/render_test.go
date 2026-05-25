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

// newRenderTestTUI builds a TUI wired to a bytes.Buffer so we can
// inspect exactly what gets written. Sets width/height so draw()
// renders a meaningful screen.
func newRenderTestTUI(t *testing.T) (*TUI, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	tui := &TUI{
		Session: ids.NewSessionID(),
		Profile: "default",
		Model:   "zai:glm-5.1",
		WebURL:  "http://localhost:8421",
		out:     buf,
	}
	tui.width = 120 // realistic terminal width; 80 truncates the URL
	tui.height = 10
	return tui, buf
}

func TestDrawIncludesStatusLine(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.draw()
	out := buf.String()
	// Status line at row 1.
	if !strings.Contains(out, "default") {
		t.Errorf("status line missing profile: %q", out)
	}
	if !strings.Contains(out, "zai:glm-5.1") {
		t.Errorf("status line missing model: %q", out)
	}
	if !strings.Contains(out, "http://localhost:8421") {
		t.Errorf("status line missing web URL: %q", out)
	}
	// ANSI clear screen + cursor home.
	if !strings.HasPrefix(out, "\x1b[2J\x1b[H") {
		t.Errorf("missing clear+home prefix: %q", out[:20])
	}
}

func TestDrawIncludesScrollbackContents(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.scrollback = []string{
		"→ user input line",
		"← assistant reply",
	}
	tui.draw()
	out := stripANSI(buf.String())
	if !strings.Contains(out, "→ user input line") {
		t.Errorf("user line missing: %q", out)
	}
	if !strings.Contains(out, "← assistant reply") {
		t.Errorf("assistant line missing: %q", out)
	}
}

func TestDrawIncludesInputPrompt(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.input = []rune("hello world")
	tui.draw()
	out := buf.String()
	// Prompt + input echoed at the bottom.
	if !strings.Contains(out, "> ") {
		t.Errorf("input prompt missing: %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("input buffer not echoed: %q", out)
	}
}

func TestDrawTruncatesLongLines(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	long := strings.Repeat("x", 500)
	tui.scrollback = []string{long}
	tui.draw()
	out := buf.String()
	// A 500-char line must shrink to fit `width` (120 here, plus a
	// little slack for ANSI codes and the ellipsis).
	for _, line := range strings.Split(out, "\r\n") {
		if strings.Contains(line, "xxxxxxxxxxxxxxxxxxxx") && len(line) > tui.width+10 {
			t.Errorf("line not truncated: len=%d width=%d", len(line), tui.width)
		}
	}
}

// TestRenderEventAppendsTextDeltaToLastAssistantLine drives the
// renderEvent path with a TextDelta envelope and verifies it lands
// in scrollback under the assistant prefix.
func TestRenderEventAppendsTextDeltaToLastAssistantLine(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), width: 80, height: 10, out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "Hello, "},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	env2, _ := control.EncodeEvent(2, control.TextDelta{Text: "world."},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env2)

	if len(tui.scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d: %v", len(tui.scrollback), tui.scrollback)
	}
	if !strings.Contains(tui.scrollback[0], "Hello, world.") {
		t.Errorf("text deltas not coalesced: %q", tui.scrollback[0])
	}
}

func TestRenderEventNewLineAfterTurnEnded(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	td, _ := control.EncodeEvent(1, control.TextDelta{Text: "first"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(td)
	end, _ := control.EncodeEvent(2, control.TurnEnded{Turn: 1, Reason: "complete"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(end)
	// Next TextDelta should start a fresh assistant line, not append.
	td2, _ := control.EncodeEvent(3, control.TextDelta{Text: "second"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(td2)

	if len(tui.scrollback) < 2 {
		t.Fatalf("expected separate scrollback lines after TurnEnded: %v", tui.scrollback)
	}
}

func TestRenderEventSurfacesError(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.Error{
		Reason: control.ReasonRateLimited, Message: "rate limit hit",
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "rate limit hit") {
		t.Errorf("error not surfaced to scrollback: %v", tui.scrollback)
	}
}

func TestHandleKeyTypingAndBackspace(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	// Type "abc".
	for _, c := range []byte{'a', 'b', 'c'} {
		if exit := tui.handleKey([]byte{c}); exit {
			t.Fatalf("typing %q triggered exit", c)
		}
	}
	if string(tui.input) != "abc" {
		t.Errorf("input = %q, want abc", string(tui.input))
	}
	// Backspace.
	tui.handleKey([]byte{0x7f})
	if string(tui.input) != "ab" {
		t.Errorf("after backspace = %q, want ab", string(tui.input))
	}
}

func TestHandleKeyEnterSubmitsViaClient(t *testing.T) {
	captured := &capturingClient{}
	tui := &TUI{
		Session: ids.NewSessionID(),
		Client:  nil, // we'll inject via the capturing client wrapper below
		out:     &bytes.Buffer{},
	}
	// We can't easily inject *inproc.Client without spinning a real
	// engine; instead we test that submit() with no client doesn't
	// panic and that the scrollback gets the user's line.
	tui.input = []rune("hello")
	tui.submit()
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "hello") {
		t.Errorf("user line not echoed to scrollback: %v", tui.scrollback)
	}
	if string(tui.input) != "" {
		t.Errorf("input not cleared after submit: %q", string(tui.input))
	}
	_ = captured
}

type capturingClient struct{ sent []string }

// TestHandleKeyCtrlDOnEmptyExits verifies the Ctrl-D-on-empty path.
func TestHandleKeyCtrlDOnEmptyExits(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	if exit := tui.handleKey([]byte{0x04}); !exit {
		t.Error("Ctrl-D on empty buffer should exit")
	}
}

func TestHandleKeyCtrlDWithBufferDoesNotExit(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	tui.input = []rune("x")
	if exit := tui.handleKey([]byte{0x04}); exit {
		t.Error("Ctrl-D with text should not exit")
	}
}

// TestRenderEventPermissionRequestedSurfaces verifies the
// PermissionRequested event lands visibly in scrollback so the user
// knows a prompt is pending.
func TestRenderEventPermissionRequestedSurfaces(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.PermissionRequested{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"rm -rf /"}`),
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "Bash") {
		t.Errorf("permission prompt not surfaced: %v", tui.scrollback)
	}
	if tui.pendingPermission == nil {
		t.Error("pendingPermission not set after PermissionRequested")
	}
}

// TestDecodePermissionAnswer covers the y/a/t/deny mapping including
// defaults. Empty / unknown input must default to deny so a stray
// Enter never silently allows a tool.
func TestDecodePermissionAnswer(t *testing.T) {
	cases := []struct {
		in    string
		dec   control.Decision
		scope control.PermitScope
	}{
		{"y", control.DecisionAllow, control.ScopeOnce},
		{"YES", control.DecisionAllow, control.ScopeOnce},
		{"a", control.DecisionAllow, control.ScopeSessionWide},
		{"t", control.DecisionAllow, control.ScopeTool},
		{"", control.DecisionDeny, control.ScopeOnce},
		{"n", control.DecisionDeny, control.ScopeOnce},
		{"garbage", control.DecisionDeny, control.ScopeOnce},
	}
	for _, c := range cases {
		dec, scope, _ := decodePermissionAnswer(c.in)
		if dec != c.dec || scope != c.scope {
			t.Errorf("decodePermissionAnswer(%q) = (%s, %s), want (%s, %s)",
				c.in, dec, scope, c.dec, c.scope)
		}
	}
}

// TestSubmitAnswersPendingPermission ensures that when a permit is
// pending, submit() consumes the input as the answer and clears the
// pending state — the input must NOT be sent as a new turn.
func TestSubmitAnswersPendingPermission(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	callID := ids.NewCallID()
	tui.pendingPermission = &control.PermissionRequested{
		CallID: callID, Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"ls"}`),
	}
	tui.input = []rune("y")
	tui.submit()
	if tui.pendingPermission != nil {
		t.Error("pendingPermission not cleared after submit")
	}
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "allow once") {
		t.Errorf("answer label not echoed: %v", tui.scrollback)
	}
}

// TestToolResultSplitsEmbeddedNewlines: a multi-line tool result is
// split into one scrollback line per source line — embedded `\n`
// inside a single scrollback entry causes the raw-mode terminal to
// emit LF without CR, producing the cascading-indent bug.
func TestToolResultSplitsEmbeddedNewlines(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: "first line\nsecond line\nthird"}},
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	// Walk scrollback and ensure NO entry contains an embedded '\n'.
	for i, line := range tui.scrollback {
		if strings.Contains(line, "\n") {
			t.Errorf("scrollback[%d] still has embedded newline: %q", i, line)
		}
	}
	// And we should have at least one entry per source line.
	var firstLine, secondLine, thirdLine bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "first line") {
			firstLine = true
		}
		if strings.Contains(line, "second line") {
			secondLine = true
		}
		if strings.Contains(line, "third") {
			thirdLine = true
		}
	}
	if !firstLine || !secondLine || !thirdLine {
		t.Errorf("multi-line result lost lines: %v", tui.scrollback)
	}
}

// TestToolResultEmptyShowsNoOutput: an empty result body renders as
// "(no output)" rather than a bare prefix.
func TestToolResultEmptyShowsNoOutput(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: ""}},
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	if len(tui.scrollback) == 0 || !strings.Contains(tui.scrollback[0], "(no output)") {
		t.Errorf("empty result not surfaced as (no output): %v", tui.scrollback)
	}
}
