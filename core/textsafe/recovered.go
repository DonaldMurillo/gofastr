package textsafe

import (
	"fmt"
	"unicode/utf8"
)

// maxRecoveredLen bounds the rendered panic value: a panic carrying a
// request body must not turn one log line into a megabyte.
const maxRecoveredLen = 4 << 10

// Recovered renders a recover() value for a log sink: fmt.Sprint,
// then every C0/DEL/C1/invisible codepoint removed, then truncated to
// 4 KiB. A panic value is whatever the panicking code held at the time,
// which on a request path is request bytes, so it gets the same scrub a
// request header does before it reaches the operator's terminal.
//
// Every in-function recover-and-log site uses this instead of logging
// the raw value (the 2026-09-06 round found nine that did not).
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
