package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestTextDeltaWithEmbeddedNewlines reproduces the screenshot bug:
// a single TextDelta payload contained markdown structure with
// embedded \n chars, which the old renderer concatenated into one
// scrollback "line" and then wrote with %s\r\n — causing the
// terminal cursor to wander columns.
func TestTextDeltaWithEmbeddedNewlines(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	env, _ := control.EncodeEvent(1, control.TextDelta{
		Text: "Great question! Here's what I can help you with:\n\n## Scaffolding\n\n- Generate entities",
	}, tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)

	// Every scrollback entry must be free of \n — that's the property
	// the renderer relies on for `%s\r\n` to behave.
	for i, line := range tui.scrollback {
		if strings.ContainsRune(line, '\n') {
			t.Errorf("scrollback[%d] contains embedded \\n: %q", i, line)
		}
		if strings.ContainsRune(line, '\r') {
			t.Errorf("scrollback[%d] contains embedded \\r: %q", i, line)
		}
	}
	// The first source line should have the `← ` prefix; the rest
	// should be continuation lines (no prefix, small indent).
	if len(tui.scrollback) == 0 || !strings.HasPrefix(tui.scrollback[0], "← ") {
		t.Errorf("first line missing assistant prefix: %v", tui.scrollback)
	}
	// "## Scaffolding" must appear as its own scrollback entry, not
	// jammed into another line.
	foundHeading := false
	for _, line := range tui.scrollback {
		if strings.TrimSpace(line) == "## Scaffolding" {
			foundHeading = true
		}
	}
	if !foundHeading {
		t.Errorf("'## Scaffolding' not isolated to its own line: %v", tui.scrollback)
	}
}

// TestTextDeltaStreamingPreservesCoalescing verifies that when a model
// streams text in many small chunks (no embedded newlines), they all
// land on the same scrollback line.
func TestTextDeltaStreamingPreservesCoalescing(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	for _, chunk := range []string{"Hello", ", ", "world", "!"} {
		env, _ := control.EncodeEvent(uint64(len(tui.scrollback)+1),
			control.TextDelta{Text: chunk}, tui.Session, ids.NewClientID(), time.Now())
		tui.renderEvent(env)
	}
	if len(tui.scrollback) != 1 {
		t.Fatalf("expected single coalesced line, got %d: %v", len(tui.scrollback), tui.scrollback)
	}
	if !strings.HasSuffix(tui.scrollback[0], "Hello, world!") {
		t.Errorf("coalesced text wrong: %q", tui.scrollback[0])
	}
}

// TestWrapLineBreaksAtWidth verifies the line-wrapping helper produces
// visual lines that all fit within the configured width.
func TestWrapLineBreaksAtWidth(t *testing.T) {
	long := strings.Repeat("x", 250)
	visual := wrapLine(long, 80)
	if len(visual) < 3 {
		t.Errorf("250-char line at width 80 wrapped into %d visual lines, want at least 3", len(visual))
	}
	for i, line := range visual {
		if len([]rune(line)) > 80 {
			t.Errorf("wrapped line %d exceeds width: len=%d", i, len([]rune(line)))
		}
	}
	// Concatenation of visual lines must equal the original.
	if strings.Join(visual, "") != long {
		t.Errorf("wrap lost data")
	}
}

// TestWrapLineShortStaysSingle: lines that already fit don't get split.
func TestWrapLineShortStaysSingle(t *testing.T) {
	got := wrapLine("hello", 80)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("short line modified: %v", got)
	}
}

// TestDrawWrapsLongScrollbackLine confirms a long logical scrollback
// entry renders as multiple visual rows on the screen, not as a
// single truncated row.
func TestDrawWrapsLongScrollbackLine(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.width = 40 // narrow so wrapping is forced
	tui.height = 10
	tui.scrollback = []string{strings.Repeat("a", 100)}
	tui.draw()
	out := buf.String()
	// We expect at least 3 visual rows of 'a's (100/40 = 2.5 → 3).
	rows := strings.Count(out, "aaaaaaaaaaaaaaaaaaaa")
	if rows < 3 {
		t.Errorf("100-char line at width 40 rendered as %d visual rows, want >= 3:\n%s", rows, out)
	}
}

// TestPageUpScrollsBack verifies the PageUp escape sequence shifts the
// window backwards through scrollback.
func TestPageUpScrollsBack(t *testing.T) {
	tui, _ := newRenderTestTUI(t)
	tui.height = 6 // scrollRows = 3
	tui.scrollback = make([]string, 50)
	for i := range tui.scrollback {
		tui.scrollback[i] = "line " + string(rune('A'+i))
	}
	if tui.scrollOffset != 0 {
		t.Fatal("default offset should be 0")
	}
	// Page Up.
	tui.handleKey([]byte{0x1b, '[', '5', '~'})
	if tui.scrollOffset == 0 {
		t.Errorf("PageUp didn't advance scrollOffset")
	}
	// Page Down brings it back.
	tui.handleKey([]byte{0x1b, '[', '6', '~'})
	if tui.scrollOffset != 0 {
		t.Errorf("PageDown didn't return to 0: got %d", tui.scrollOffset)
	}
}

// TestSubmitResetsScrollOffset: typing a new prompt should pull the
// view back to the tail.
func TestSubmitResetsScrollOffset(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	tui.input = []rune("hi")
	tui.scrollOffset = 50
	tui.submit()
	if tui.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d after submit, want 0", tui.scrollOffset)
	}
}

// TestTurnEndedClosesAssistantLine: after TurnEnded, the next TextDelta
// should start a fresh `← ` line, not continue the previous one.
func TestTurnEndedClosesAssistantLine(t *testing.T) {
	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	d1, _ := control.EncodeEvent(1, control.TextDelta{Text: "first turn"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(d1)
	end, _ := control.EncodeEvent(2, control.TurnEnded{Turn: 1, Reason: "complete"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(end)
	d2, _ := control.EncodeEvent(3, control.TextDelta{Text: "second turn"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(d2)

	// Find the "second turn" line and verify it has the assistant prefix.
	foundFresh := false
	for _, line := range tui.scrollback {
		if strings.HasPrefix(line, "← ") && strings.Contains(line, "second turn") {
			foundFresh = true
		}
	}
	if !foundFresh {
		t.Errorf("second turn didn't open a fresh assistant line: %v", tui.scrollback)
	}
}
