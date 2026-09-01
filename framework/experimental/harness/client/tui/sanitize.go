package tui

import "strings"

// sanitizeAgentText strips the bytes a terminal interprets as commands
// rather than content, from a string that came from OUTSIDE this process.
//
// Model output, tool results, and upstream error strings all reach
// scrollback verbatim. An ESC in any of them is not text — it opens a
// control sequence the emulator executes. OSC 52 writes the user's system
// clipboard, OSC 0 rewrites the window title, CSI ?1049h flips to the
// alternate screen and takes the UI away, CSI ?1000h turns on mouse
// reporting, and a bare CR plus BEL overwrites the row that was just
// drawn so the transcript no longer shows what happened.
//
// The whole C0 range goes, plus DEL, with two exceptions. Tab is layout.
// Newline survives because every caller splits on it to build rows, so
// stripping it would join the model's paragraphs into one line; it never
// reaches a stored row, because the split consumes it.
//
// The TUI's OWN lines are not run through this. The welcome banner and
// the turn separators carry deliberate SGR styling, and stripping ESC
// from those would leave a literal "[1m" on screen. The rule is "text
// this process did not write", not "every line".
func sanitizeAgentText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if isControlByte(r) {
			return -1
		}
		return r
	}, s)
}

// isControlByte reports whether r is a C0 control or DEL, excluding tab.
func isControlByte(r rune) bool {
	return (r < 0x20 && r != '\t') || r == 0x7f
}
