package textsafe

import (
	"fmt"
	"unicode/utf8"
)

const maxRecoveredLen = 4 << 10

// Recovered renders a recover() value for a log: fmt.Sprint, StripUnsafe, then cut to 4 KiB on a rune boundary.
func Recovered(v any) string {
	s := StripUnsafe(fmt.Sprint(v))
	if len(s) <= maxRecoveredLen {
		return s
	}
	cut := maxRecoveredLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…(truncated)"
}
