// Package textcut cuts a string to a byte budget on a rune boundary.
package textcut

import "unicode/utf8"

// Ellipsis returns s, or its first max bytes cut back to a rune boundary and followed by "…".
func Ellipsis(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
