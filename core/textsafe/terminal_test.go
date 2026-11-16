package textsafe

import "testing"

// One table for everything a terminal must not be handed; newline and tab stay.
func TestScrubTerminal(t *testing.T) {
	for in, want := range map[string]string{
		"plain line\n\tindented\n":              "plain line\n\tindented\n",
		"a\x1b]0;title\x07b\x7fc":               "a]0;titlebc",
		"line one\roverpaint":                   "line oneoverpaint",
		"GET /x\u009b31mred\u009dt\u009c\x1b[m": "GET /x31mredt[m",
		"a\x9bb\xffc":                           "abc",
		"safe\u202egnp.exe":                     "safegnp.exe",
		"zero\u200bwidth\ufeff":                 "zerowidth",
		"one\u2028two\u2029three\u0085four":     "onetwothreefour",
		"nul\x00vt\x0bff\x0c":                   "nulvtff",
		"h\u00e9llo \u2713":                     "h\u00e9llo \u2713",
	} {
		if got := ScrubTerminal(in); got != want {
			t.Errorf("ScrubTerminal(%q) = %q, want %q", in, got, want)
		}
	}
}
