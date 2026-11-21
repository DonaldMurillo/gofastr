package textsafe

import (
	"strings"
	"testing"
)

func TestHasControlBytesFlagsC0AndDEL(t *testing.T) {
	for _, s := range []string{"\x00", "a\x01b", "tab\there", "nl\n", "cr\r", "esc\x1b[31m", "del\x7f", "\x1f"} {
		if !HasControlBytes(s) {
			t.Errorf("HasControlBytes(%q) = false, want true", s)
		}
	}
}

func TestHasControlBytesIgnoresPrintableAndUnicode(t *testing.T) {
	// A valid UTF-8 sequence never holds a byte below 0x20, so C1
	// controls and bidi overrides are out of scope by contract.
	for _, s := range []string{"", "plain ascii", "space only ", "~", "héllo", "日本語", "\u0085", "\u202e", "\u200b"} {
		if HasControlBytes(s) {
			t.Errorf("HasControlBytes(%q) = true, want false", s)
		}
	}
}

func TestSanitizeControlBytesRemovesOnlyControls(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"clean":                "clean",
		"a\r\nb":               "ab",
		"\x00lead":             "lead",
		"trail\x7f":            "trail",
		"\x1b[31mred\x1b[0m":   "[31mred[0m",
		"hé\tllo":              "héllo",
		"\u202e日本\n語":          "\u202e日本語",
		"\x01\x02\x03\x1f\x7f": "",
	}
	for in, want := range cases {
		if got := SanitizeControlBytes(in); got != want {
			t.Errorf("SanitizeControlBytes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeControlBytesCleanFastPathReturnsSameString(t *testing.T) {
	in := strings.Repeat("abc", 10)
	if got := SanitizeControlBytes(in); got != in {
		t.Fatalf("clean input changed: %q", got)
	}
}

func TestScrubControlBytesPercentEncodesC0AndDEL(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"clean":        "clean",
		"a\r\nb":       "a%0d%0ab",
		"\x00":         "%00",
		"\x7f":         "%7f",
		"\x1b[31m":     "%1b[31m",
		"\x01\x04\x1c": "%01%04%1c",
		"space ok":     "space ok",
		"tilde~ok":     "tilde~ok",
	}
	for in, want := range cases {
		if got := ScrubControlBytes(in); got != want {
			t.Errorf("ScrubControlBytes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubControlBytesEncodesC1AndBidiRunes(t *testing.T) {
	cases := map[string]string{
		"\u009b":   "%c2%9b", // 8-bit CSI as a rune
		"\u0085":   "%c2%85", // NEL
		"\u202e":   "%e2%80%ae",
		"\u200b":   "%e2%80%8b",
		"a\u202eb": "a%e2%80%aeb",
		"日本語":      "日本語",
		"héllo":    "héllo",
		"é\u009bé": "é%c2%9bé",
	}
	for in, want := range cases {
		if got := ScrubControlBytes(in); got != want {
			t.Errorf("ScrubControlBytes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubControlBytesStrayHighBytes(t *testing.T) {
	// A bare 0x9B on the wire is the 8-bit C1 CSI: encoded. A stray
	// byte above 0x9F is not a control and passes through untouched.
	if got := ScrubControlBytes("a\x9bb"); got != "a%9bb" {
		t.Errorf("stray C1 byte: got %q, want %q", got, "a%9bb")
	}
	if got := ScrubControlBytes("a\x80b"); got != "a%80b" {
		t.Errorf("stray 0x80: got %q, want %q", got, "a%80b")
	}
	if got := ScrubControlBytes("a\xffb"); got != "a\xffb" {
		t.Errorf("stray 0xff: got %q, want passthrough", got)
	}
	if got := ScrubControlBytes("a\xe9b"); got != "a\xe9b" {
		t.Errorf("stray 0xe9: got %q, want passthrough", got)
	}
}

// Outputs captured from the implementation before the shared
// AppendPercentByte step; they pin byte-identity across C0, DEL, C1 runes,
// invisible runes, valid and invalid UTF-8.
func TestScrubControlBytesPinnedCorpus(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"plain ascii 123":         "plain ascii 123",
		"\x00nul":                 "%00nul",
		"a\x01\x02\x1f b":         "a%01%02%1f b",
		"cr lf\r\nnl":             "cr lf%0d%0anl",
		"esc\x1b[31m del\x7f":     "esc%1b[31m del%7f",
		"\u0080\u0085\u009b":      "%c2%80%c2%85%c2%9b",
		"zw\u200bsp":              "zw%e2%80%8bsp",
		"rlo \u202e":              "rlo %e2%80%ae",
		"sep\u2028\u2029":         "sep%e2%80%a8%e2%80%a9",
		"héllo 😀":                 "héllo 😀",
		"日本語":                     "日本語",
		"stray\x80\x9bhigh":       "stray%80%9bhigh",
		"\xff\xfe raw":            "\xff\xfe raw",
		"trunc\xc3ated":           "trunc\xc3ated",
		"mix\x00\u009b\u200b\xff": "mix%00%c2%9b%e2%80%8b\xff",
		"\xed\xa0\x80surrogate":   "\xed\xa0%80surrogate",
	}
	for in, want := range cases {
		if got := ScrubControlBytes(in); got != want {
			t.Errorf("ScrubControlBytes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubControlBytesCleanASCIIFastPath(t *testing.T) {
	in := "GET /users/42?page=1 HTTP/1.1"
	if got := ScrubControlBytes(in); got != in {
		t.Fatalf("clean ASCII changed: %q", got)
	}
}

func TestNeedsControlScrubIsSupersetOfEncoder(t *testing.T) {
	// The probe must flag everything the encoder rewrites. Walk every
	// byte value plus the unsafe rune set: whenever the encoder changes
	// the input, the probe must have said yes.
	for b := 0; b < 256; b++ {
		s := "x" + string([]byte{byte(b)}) + "y"
		if ScrubControlBytes(s) != s && !needsControlScrub(s) {
			t.Errorf("byte %#02x: encoder rewrote but probe said clean", b)
		}
	}
	for _, r := range []rune{0x85, 0x9b, 0x200b, 0x200e, 0x202e, 0x2066, 0xfeff} {
		s := "x" + string(r) + "y"
		if ScrubControlBytes(s) != s && !needsControlScrub(s) {
			t.Errorf("rune %U: encoder rewrote but probe said clean", r)
		}
	}
}
