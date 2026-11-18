package tui

import (
	"fmt"
	"strings"
)

type modalPanel struct {
	title string
	lines []string
}

func (t *TUI) openModal(title string, lines []string) {
	clean := make([]string, len(lines))
	for i, ln := range lines {
		clean[i] = sanitizeModalText(ln)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modal = &modalPanel{title: sanitizeModalText(title), lines: clean}
}

func (t *TUI) closeModal() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modal = nil
}

func (t *TUI) modalKey(bs []byte) bool {
	if len(bs) == 0 {
		return false
	}
	if bs[0] == 0x1b && len(bs) == 1 {
		t.closeModal()
		return true
	}
	switch bs[0] {
	case 0x0d, 0x0a, 'q', 'Q':
		t.closeModal()
		return true
	}
	return true
}

func (t *TUI) drawModal() {
	if t.modal == nil {
		return
	}
	maxW := min(max(t.width-8, 20), 80)
	innerW := max(maxW-4, 10)
	var bodyVisual []string
	for _, ln := range t.modal.lines {
		if ln == "" {
			bodyVisual = append(bodyVisual, "")
			continue
		}
		bodyVisual = append(bodyVisual, wrapLine(ln, innerW)...)
	}
	bodyH := len(bodyVisual)
	totalH := bodyH + 4
	maxH := t.height - 6
	if totalH > maxH {
		totalH = maxH
		bodyH = max(totalH-4, 0)
		bodyVisual = bodyVisual[:bodyH]
	}
	startRow := 1 + (t.height-totalH)/2
	startCol := 1 + (t.width-maxW)/2
	move := func(r, c int) string { return fmt.Sprintf("\x1b[%d;%dH", r, c) }
	pad := func(s string, w int) string {
		l := runeLen(s)
		if l >= w {
			return s
		}
		return s + strings.Repeat(" ", w-l)
	}
	titlePart := " " + t.modal.title + " "
	dashLen := max(maxW-2-runeLen(titlePart)-1, 0)
	top := "╭─" + titlePart + strings.Repeat("─", dashLen) + "╮"
	_, _ = fmt.Fprint(t.out, move(startRow, startCol), ansiBold, top, ansiReset)
	_, _ = fmt.Fprint(t.out, move(startRow+1, startCol),
		"│"+strings.Repeat(" ", maxW-2)+"│")
	for i, ln := range bodyVisual {
		_, _ = fmt.Fprint(t.out, move(startRow+2+i, startCol),
			"│ "+pad(ln, maxW-4)+" │")
	}
	_, _ = fmt.Fprint(t.out, move(startRow+2+bodyH, startCol),
		"│"+strings.Repeat(" ", maxW-2)+"│")
	hint := "Press Esc or Enter to close"
	_, _ = fmt.Fprint(t.out,
		move(startRow+3+bodyH, startCol),
		"│ ", ansiDim, pad(hint, maxW-4), ansiReset, " │")
	_, _ = fmt.Fprint(t.out,
		move(startRow+4+bodyH, startCol),
		ansiBold, "╰"+strings.Repeat("─", maxW-2)+"╯", ansiReset)
}
