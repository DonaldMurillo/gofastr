package textcut

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEllipsisCutsOnARuneBoundary(t *testing.T) {
	got := Ellipsis(strings.Repeat("é", 200), 121) // 400 bytes, 2-byte runes
	if !utf8.ValidString(got) {
		t.Fatalf("Ellipsis cut mid-rune: %q", got)
	}
	if want := strings.Repeat("é", 60) + "…"; got != want {
		t.Fatalf("Ellipsis = %q, want %q", got, want)
	}
}

func TestEllipsisKeepsStringsAtOrUnderMax(t *testing.T) {
	for name, s := range map[string]string{
		"short":          "SELECT 1",
		"exactly at max": strings.Repeat("a", 10),
	} {
		if got := Ellipsis(s, 10); got != s {
			t.Errorf("%s: Ellipsis changed it: %q", name, got)
		}
	}
}
