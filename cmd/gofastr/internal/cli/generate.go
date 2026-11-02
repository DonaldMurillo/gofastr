package cli

import (
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ScrubTerminalOutput strips the control bytes that let a subprocess drive
// the operator's terminal rather than write to it, plus the display-spoofing
// set: the 8-bit C1 spellings (U+009B CSI arrives without an ESC prefix),
// bidi overrides, and zero-width runes (core/textsafe, 2026-09-05
// red-probe round). Mirrors codegen's stderr scrub
// (codegen/extension_command.go scrubTerminalBytes) for the diagnostic
// path, which arrives as JSON rather than as raw stderr bytes. \n and \t
// stay: ordinary formatting. A byte that is not valid UTF-8 goes too: a
// bare 0x9b is the 8-bit CSI to a terminal that is not in UTF-8 mode,
// and ranging over a string reads it as U+FFFD, which no rune check
// refuses.
func ScrubTerminalOutput(s string) string {
	drop := func(r rune, size int) bool {
		return (r == utf8.RuneError && size == 1) || (r != '\n' && r != '\t' && textsafe.IsUnsafe(r))
	}
	clean := true
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if drop(r, size) {
			clean = false
			break
		}
		i += size
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !drop(r, size) {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}
