package tui

import "strings"

func (t *TUI) handleEscape(bs []byte) {
	s := string(bs)
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
		t.scrollDelta(+1)
	case s == "\x1b[B":
		t.scrollDelta(-1)
	case s == "\x1b[5~":
		t.scrollDelta(+t.pageStep())
	case s == "\x1b[6~":
		t.scrollDelta(-t.pageStep())
	case s == "\x1b[H" || s == "\x1b[1~":
		t.scrollHome()
	case s == "\x1b[F" || s == "\x1b[4~":
		t.scrollEnd()
	default:
		if strings.HasPrefix(s, "\x1b[<") {
			switch {
			case strings.Contains(s, "<64;") && strings.HasSuffix(s, "M"):
				t.scrollDelta(+3)
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
}

func (t *TUI) scrollHome() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scrollOffset = 1 << 30
}

func (t *TUI) scrollEnd() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scrollOffset = 0
}

func (t *TUI) pageStep() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	step := max(t.height-3, 1)
	return step
}
