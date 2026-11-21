package email

import "testing"

// Outputs captured from scrubHeaderValue before it started sharing
// textsafe's percent-byte encoder; they pin byte-identity across C0, DEL,
// C1 runes, invisible runes, valid and invalid UTF-8, including the
// pass-through of stray high bytes the C1 gate never rewrites.
func TestScrubHeaderValuePinnedCorpus(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"plain ascii 123":         "plain ascii 123",
		"\x00nul":                 "%00nul",
		"a\x01\x02\x1f b":         "a%01%02%1f b",
		"cr lf\r\nnl":             "cr lf%0d%0anl",
		"esc\x1b[31m del\x7f":     "esc%1b[31m del%7f",
		"\u0080\u0085\u009b":      "",
		"zw\u200bsp":              "zwsp",
		"rlo \u202e":              "rlo ",
		"sep\u2028\u2029":         "sep\u2028\u2029",
		"héllo 😀":                 "héllo 😀",
		"日本語":                     "日本語",
		"stray\x80\x9bhigh":       "stray\x80\x9bhigh",
		"\xff\xfe raw":            "\xff\xfe raw",
		"trunc\xc3ated":           "trunc\xc3ated",
		"mix\x00\u009b\u200b\xff": "mix%00\ufffd",
		"\xed\xa0\x80surrogate":   "\xed\xa0\x80surrogate",
	}
	for in, want := range cases {
		if got := scrubHeaderValue(in); got != want {
			t.Errorf("scrubHeaderValue(%q) = %q, want %q", in, got, want)
		}
	}
}
