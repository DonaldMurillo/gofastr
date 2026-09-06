package tui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// sanitizeAgentText strips the bytes and runes a terminal interprets as
// commands (or swallows as invisible) from a string that came from
// OUTSIDE this process.
//
// Model output, tool results, and upstream error strings all reach
// scrollback verbatim. An ESC in any of them is not text — it opens a
// control sequence the emulator executes. OSC 52 writes the user's system
// clipboard, OSC 0 rewrites the window title, CSI ?1049h flips to the
// alternate screen and takes the UI away, CSI ?1000h turns on mouse
// reporting, and a bare CR plus BEL overwrites the row that was just
// drawn so the transcript no longer shows what happened.
//
// The C0 range and DEL go, with two exceptions. Tab is layout. Newline
// survives because every caller splits on it to build rows, so stripping
// it would join the model's paragraphs into one line; it never reaches a
// stored row, because the split consumes it.
//
// C0/DEL alone is one encoding step from useless: the 8-bit C1 aliases
// (U+009B CSI, U+009D OSC, U+009C ST) drive the same sequences in
// C1-aware terminals, and the invisible/bidi set (RLO U+202E, isolates
// U+2066–U+2069, zero-width U+200B/U+FEFF, ...) reorders or hides the
// row so the transcript stops showing what happened. The whole set is
// the shared predicate in core/textsafe; this scrub is the terminal-sink
// spelling of it.
//
// The TUI's OWN lines are not run through this. The welcome banner and
// the turn separators carry deliberate SGR styling, and stripping ESC
// from those would leave a literal "[1m" on screen. The rule is "text
// this process did not write", not "every line".
func sanitizeAgentText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if textsafe.IsUnsafe(r) {
			return -1
		}
		return r
	}, s)
}
