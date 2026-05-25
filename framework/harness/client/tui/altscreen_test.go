package tui

import (
	"bytes"
	"strings"
	"testing"
)

// TestAltScreenEnablesAltScrollNotMouseTracking: when the TUI enters
// the alt screen, it should NOT enable SGR mouse tracking
// (?1000h / ?1006h) — that mode steals terminal text selection
// because every click/drag becomes an app-bound CSI event. Instead,
// it should enable ?1007h "alternate scroll" which converts wheel
// events to Up/Down arrow keys. The TUI's existing arrow handler
// then scrolls, AND the user can still click-drag to select text
// for copy.
//
// Failing before the mode change; passing after.
func TestAltScreenEnablesAltScrollNotMouseTracking(t *testing.T) {
	buf := &bytes.Buffer{}
	tui := &TUI{out: buf}
	tui.enterAltScreen()
	got := buf.String()

	// Must enable alt scroll so wheel still works for scrolling.
	if !strings.Contains(got, "\x1b[?1007h") {
		t.Errorf("enterAltScreen did not enable alternate scroll (?1007h): %q", got)
	}
	// Must NOT enable mouse tracking (the cause of broken text selection).
	for _, bad := range []string{"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h", "\x1b[?1006h"} {
		if strings.Contains(got, bad) {
			t.Errorf("enterAltScreen enabled mouse tracking %q — selection will be broken", bad)
		}
	}
}

// TestExitAltScreenDisablesAltScroll: leaving the alt screen must
// restore the user's terminal — disable alt-scroll cleanly so the
// next shell session isn't stuck in a weird state.
func TestExitAltScreenDisablesAltScroll(t *testing.T) {
	buf := &bytes.Buffer{}
	tui := &TUI{out: buf}
	tui.exitAltScreen()
	got := buf.String()
	if !strings.Contains(got, "\x1b[?1007l") {
		t.Errorf("exitAltScreen did not disable alternate scroll (?1007l): %q", got)
	}
	// Defense in depth: even if a prior enter ever turned on mouse
	// tracking (e.g., a future profile), exit must always disable it.
	if !strings.Contains(got, "\x1b[?1049l") {
		t.Errorf("exitAltScreen did not leave the alt screen (?1049l): %q", got)
	}
}
