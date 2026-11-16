package textsafe

import (
	"strings"
	"unicode/utf8"
)

// ScrubTerminal removes from s what would drive or spoof a terminal it is
// printed to: every IsUnsafe rune except newline and tab, and every byte
// that is not valid UTF-8 (a bare 0x9b is CSI to an 8-bit terminal).
func ScrubTerminal(s string) string {
	drop := func(r rune, size int) bool {
		return (r == utf8.RuneError && size == 1) || (r != '\n' && r != '\t' && IsUnsafe(r))
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
