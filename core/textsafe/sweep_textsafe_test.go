package textsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortFlagTagRunIsStripped(t *testing.T) {
	for _, s := range []string{flagStr + cancelStr, flagStr + tagged("g") + cancelStr, flagStr + tagged("gb") + cancelStr} {
		if got := StripUnsafe(s); got != flagStr {
			t.Errorf("StripUnsafe(%q) = %q, want the bare flag", s, got)
		}
	}
}

func TestRecoveredCapsAtFourKiBOnARuneBoundary(t *testing.T) {
	got := Recovered("a" + strings.Repeat("é", 4096))
	body, ok := strings.CutSuffix(got, "…(truncated)")
	if !ok || len(body) > 4096 || len(body) < 4090 || !utf8.ValidString(body) {
		t.Fatalf("Recovered = %d bytes (valid %v, marker %v), want at most 4096 valid bytes and the marker", len(body), utf8.ValidString(body), ok)
	}
}

func TestInvisibleSetIsExactlyTheNamedCodepoints(t *testing.T) {
	named := []rune{0x200B, 0x200C, 0x200D, 0x200E, 0x200F, 0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0x061C, 0x180E, 0xFEFF, 0x2060, 0x2061, 0x2062, 0x2063, 0x2064, 0x202F, 0x115F, 0x1160, 0x3164, 0xFFA0}
	for _, r := range named {
		if !IsInvisible(r) {
			t.Errorf("IsInvisible(%U) = false", r)
		}
	}
	for _, r := range []rune{0x0301, 0x00E9, 0x00A0, 0x2000, 'a'} {
		if IsInvisible(r) {
			t.Errorf("IsInvisible(%U) = true, want the set kept narrow", r)
		}
	}
}
