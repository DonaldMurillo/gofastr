package tui

import (
	"bytes"
	"strings"
	"testing"
)

// TestWordWrapBreaksAtSpaces verifies the wrap helper picks
// whitespace, not mid-word, when a break is possible within width.
func TestWordWrapBreaksAtSpaces(t *testing.T) {
	input := "authentication, database connections, or configuration, I can provide accurate, framework-specific guidance and code snippets without"
	out := wrapLine(input, 40)
	if len(out) < 3 {
		t.Fatalf("expected multiple wrapped lines, got %d", len(out))
	}
	for i, line := range out {
		if len([]rune(line)) > 40 {
			t.Errorf("line %d exceeds width: len=%d %q", i, len([]rune(line)), line)
		}
		// No line should end with a letter that's the start of a
		// word (the bug from the screenshot: "authent\nication"
		// breaks 'authentication' mid-word).
		if strings.HasSuffix(line, "authent") {
			t.Errorf("line %d breaks 'authentication' mid-word: %q", i, line)
		}
	}
}

// TestWordWrapNeverBreaksShortWord ensures common short words land
// whole on one line.
func TestWordWrapNeverBreaksShortWord(t *testing.T) {
	input := "the quick brown fox jumps over the lazy dog"
	out := wrapLine(input, 20)
	rejoined := strings.Join(out, " ")
	// Rejoining with spaces should give back the original text
	// (modulo trimmed trailing spaces on individual lines).
	if !strings.Contains(rejoined, "the quick brown") {
		t.Errorf("words got split: %v", out)
	}
	for _, w := range []string{"quick", "brown", "jumps", "over"} {
		if !strings.Contains(rejoined, w) {
			t.Errorf("word %q lost during wrap: %v", w, out)
		}
	}
}

// TestWordWrapHardBreakOnLongToken: a token longer than width must
// hard-break (otherwise we'd loop forever).
func TestWordWrapHardBreakOnLongToken(t *testing.T) {
	long := strings.Repeat("x", 100)
	out := wrapLine(long, 40)
	if len(out) < 2 {
		t.Errorf("long token didn't hard-break: %v", out)
	}
}

// TestArrowUpScrollsBack: pressing Up arrow shifts the view back by
// one visual line (per the user's expectation that arrow keys are
// the natural scroll keys).
func TestArrowUpScrollsBack(t *testing.T) {
	tui := &TUI{out: &bytes.Buffer{}}
	tui.handleKey([]byte{0x1b, '[', 'A'})
	if tui.scrollOffset != 1 {
		t.Errorf("Up arrow: scrollOffset = %d, want 1", tui.scrollOffset)
	}
	tui.handleKey([]byte{0x1b, '[', 'A'})
	if tui.scrollOffset != 2 {
		t.Errorf("two Ups: scrollOffset = %d, want 2", tui.scrollOffset)
	}
	tui.handleKey([]byte{0x1b, '[', 'B'}) // Down
	if tui.scrollOffset != 1 {
		t.Errorf("Down arrow: scrollOffset = %d, want 1", tui.scrollOffset)
	}
}

// TestMouseWheelScroll: SGR wheel events parsed and applied.
func TestMouseWheelScroll(t *testing.T) {
	tui := &TUI{out: &bytes.Buffer{}}
	// Wheel up: ESC [ < 64 ; X ; Y M  → scroll back 3 lines.
	tui.handleKey([]byte("\x1b[<64;10;20M"))
	if tui.scrollOffset != 3 {
		t.Errorf("wheel up: scrollOffset = %d, want 3", tui.scrollOffset)
	}
	tui.handleKey([]byte("\x1b[<65;10;20M")) // wheel down
	if tui.scrollOffset != 0 {
		t.Errorf("wheel down: scrollOffset = %d, want 0", tui.scrollOffset)
	}
}

// TestHomeKeyScrollsToTop: ESC[H must scroll all the way back.
func TestHomeKeyScrollsToTop(t *testing.T) {
	tui, _ := newRenderTestTUI(t)
	tui.scrollback = make([]string, 200)
	for i := range tui.scrollback {
		tui.scrollback[i] = "line"
	}
	tui.handleKey([]byte{0x1b, '[', 'H'})
	tui.draw() // clamp happens during draw
	if tui.scrollOffset == 0 {
		t.Errorf("Home key didn't move scroll position")
	}
}

// TestEndKeyReturnsToTail.
func TestEndKeyReturnsToTail(t *testing.T) {
	tui := &TUI{out: &bytes.Buffer{}}
	tui.scrollOffset = 50
	tui.handleKey([]byte{0x1b, '[', 'F'})
	if tui.scrollOffset != 0 {
		t.Errorf("End key: scrollOffset = %d, want 0", tui.scrollOffset)
	}
}
