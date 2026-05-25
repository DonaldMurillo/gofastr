package tui

// Modal overlay system. A modal is a centered framed panel drawn on
// top of the scrollback. While active, all keys flow through the
// modal first; the standard ones (Esc, Enter, q) dismiss it. The
// goal here is a self-contained primitive future features can lean
// on — e.g. interactive model picker, diff preview, confirmation
// prompts — without re-implementing overlay geometry each time.
//
// v1 supports a static text panel (title + body lines). Interactive
// pickers are roadmap.

import (
	"fmt"
	"strings"
)

// modalPanel is a static, dismissible overlay with a title and body.
type modalPanel struct {
	title string
	lines []string
}

// openModal raises a modal with the given title + body lines. Safe to
// call from outside renderEvent; takes the mutex.
func (t *TUI) openModal(title string, lines []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modal = &modalPanel{title: title, lines: lines}
}

// closeModal dismisses any active modal.
func (t *TUI) closeModal() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modal = nil
}

// modalKey reports whether the given key dismisses the modal. Returns
// (handled, exit). The TUI never exits via a modal key; the exit
// return value is reserved for symmetry with handleKey.
func (t *TUI) modalKey(bs []byte) bool {
	if len(bs) == 0 {
		return false
	}
	// Esc, Enter, 'q', 'Q' — all dismiss.
	if bs[0] == 0x1b && len(bs) == 1 {
		t.closeModal()
		return true
	}
	switch bs[0] {
	case 0x0d, 0x0a, 'q', 'Q':
		t.closeModal()
		return true
	}
	// Swallow everything else (so cursor keys don't move scroll, etc.)
	return true
}

// drawModal paints the centered framed overlay. Called from draw()
// after scrollback but before the input box, so the modal sits above
// content but doesn't cover the input area (the user can still see
// what they were typing if a modal pops mid-input).
//
// Caller must hold t.mu.
func (t *TUI) drawModal() {
	if t.modal == nil {
		return
	}
	// Geometry: width = min(80, t.width-8). Height = lines + 4 (title
	// row, blank, body lines, blank, esc hint).
	maxW := t.width - 8
	if maxW < 20 {
		maxW = 20
	}
	if maxW > 80 {
		maxW = 80
	}
	// Wrap each body line to maxW-4 (frame chars + padding).
	innerW := maxW - 4
	if innerW < 10 {
		innerW = 10
	}
	var bodyVisual []string
	for _, ln := range t.modal.lines {
		if ln == "" {
			bodyVisual = append(bodyVisual, "")
			continue
		}
		bodyVisual = append(bodyVisual, wrapLine(ln, innerW)...)
	}
	bodyH := len(bodyVisual)
	totalH := bodyH + 4 // title, blank, body, blank, esc-hint
	maxH := t.height - 6
	if totalH > maxH {
		totalH = maxH
		// Clip body lines from the bottom.
		bodyH = totalH - 4
		if bodyH < 0 {
			bodyH = 0
		}
		bodyVisual = bodyVisual[:bodyH]
	}
	// Compute top-left in absolute coords (1-indexed).
	startRow := 1 + (t.height-totalH)/2
	startCol := 1 + (t.width-maxW)/2
	// Helpers.
	move := func(r, c int) string { return fmt.Sprintf("\x1b[%d;%dH", r, c) }
	pad := func(s string, w int) string {
		l := runeLen(s)
		if l >= w {
			return s
		}
		return s + strings.Repeat(" ", w-l)
	}
	// Top border with title baked in: ┌─ Title ──────...
	titlePart := " " + t.modal.title + " "
	dashLen := maxW - 2 - runeLen(titlePart) - 1
	if dashLen < 0 {
		dashLen = 0
	}
	top := "╭─" + titlePart + strings.Repeat("─", dashLen) + "╮"
	_, _ = fmt.Fprint(t.out, move(startRow, startCol), ansiBold, top, ansiReset)
	// Blank row.
	_, _ = fmt.Fprint(t.out, move(startRow+1, startCol),
		"│"+strings.Repeat(" ", maxW-2)+"│")
	// Body rows.
	for i, ln := range bodyVisual {
		_, _ = fmt.Fprint(t.out, move(startRow+2+i, startCol),
			"│ "+pad(ln, maxW-4)+" │")
	}
	// Blank + hint row.
	_, _ = fmt.Fprint(t.out, move(startRow+2+bodyH, startCol),
		"│"+strings.Repeat(" ", maxW-2)+"│")
	hint := "Press Esc or Enter to close"
	_, _ = fmt.Fprint(t.out,
		move(startRow+3+bodyH, startCol),
		"│ ", ansiDim, pad(hint, maxW-4), ansiReset, " │")
	// Bottom border.
	_, _ = fmt.Fprint(t.out,
		move(startRow+4+bodyH, startCol),
		ansiBold, "╰"+strings.Repeat("─", maxW-2)+"╯", ansiReset)
}
