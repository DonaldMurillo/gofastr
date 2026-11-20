package textsafe

import (
	"strings"
	"unicode/utf8"
)

// ScrubTerminal removes every IsUnsafe rune except newline and tab, and every invalid UTF-8 byte, from s.
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
