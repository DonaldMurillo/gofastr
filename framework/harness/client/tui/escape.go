package tui

import "strings"

// handleEscape interprets a CSI / OSC sequence read from stdin.
//
// Recognized:
//
//	ESC [ A           Up arrow      → scroll back 1 visual line
//	ESC [ B           Down arrow    → scroll forward 1 visual line
//	ESC [ 5 ~         Page Up       → scroll back one page
//	ESC [ 6 ~         Page Down     → scroll forward one page
//	ESC [ H | 1 ~     Home          → scroll to top
//	ESC [ F | 4 ~     End           → scroll to tail
//	ESC [ < 64 ; X ; Y M   Mouse wheel up   (SGR mouse)
//	ESC [ < 65 ; X ; Y M   Mouse wheel down
//
// Anything else (left/right arrows, function keys, full mouse moves) is
// currently ignored — the inputs that matter for v0.1 are typing,
// submission, and scrollback navigation.
func (t *TUI) handleEscape(bs []byte) {
	s := string(bs)
	// When a slash popup is active, Up/Down navigate the candidate
	// list instead of scrolling scrollback.
	if (s == "\x1b[A" || s == "\x1b[B") && t.popupActive() {
		if s == "\x1b[A" {
			t.popupMove(-1)
		} else {
			t.popupMove(+1)
		}
		return
	}
	switch {
	case s == "\x1b[A":
		t.scrollDelta(+1) // up = view older
	case s == "\x1b[B":
		t.scrollDelta(-1) // down = view newer
	case s == "\x1b[5~":
		t.scrollDelta(+t.pageStep())
	case s == "\x1b[6~":
		t.scrollDelta(-t.pageStep())
	case s == "\x1b[H" || s == "\x1b[1~":
		t.scrollHome()
	case s == "\x1b[F" || s == "\x1b[4~":
		t.scrollEnd()
	default:
		// SGR mouse-wheel events: ESC[<64;X;Y M (wheel up),
		// ESC[<65;X;Y M (wheel down). The terminator can also be 'm'
		// for mouse-up but wheel events don't have an up phase.
		if strings.HasPrefix(s, "\x1b[<") {
			switch {
			case strings.Contains(s, "<64;") && strings.HasSuffix(s, "M"):
				t.scrollDelta(+3) // 3 lines per notch — common default
			case strings.Contains(s, "<65;") && strings.HasSuffix(s, "M"):
				t.scrollDelta(-3)
			}
		}
	}
}

func (t *TUI) scrollDelta(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scrollOffset += delta
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}
	// Upper clamp happens in draw() (depends on visual line count).
}

func (t *TUI) scrollHome() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scrollOffset = 1 << 30 // draw() clamps to maxOffset
}

func (t *TUI) scrollEnd() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scrollOffset = 0
}

func (t *TUI) pageStep() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	step := t.height - 3
	if step < 1 {
		step = 1
	}
	return step
}
