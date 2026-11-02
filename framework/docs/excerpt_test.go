package docs

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// excerptAround is pure; its edge branches were covered only by whatever
// long lines the embedded topics happened to contain, so a content edit
// could drop them. Each branch is pinned here directly.
func TestExcerptAround(t *testing.T) {
	long := strings.Repeat("a", 40) + "NEEDLE" + strings.Repeat("b", 40) // 86 chars
	cases := []struct {
		name, line, needle string
		cap                int
		want               string
	}{
		{"short line returned whole", "a needle here", "needle", 80, "a needle here"},
		{"middle match cut both sides", long, "needle", 16, "…aaaaaaaaNEEDLEbbbbbbbb…"},
		{"match at the start shifts right", "NEEDLE" + strings.Repeat("c", 30), "needle", 10, "NEEDLEcccccccccc…"},
		{"match at the end shifts left", strings.Repeat("d", 30) + "NEEDLE", "needle", 10, "…ddddddddddNEEDLE"},
		{"cap wider than the tail clamps both ends", "xNEEDLEy", "needle", 7, "xNEEDLEy"},
	}
	for _, c := range cases {
		got := excerptAround(c.line, strings.ToLower(c.line), c.needle, c.cap)
		if got != c.want {
			t.Errorf("%s: excerptAround(%q, %q, %d) = %q, want %q", c.name, c.line, c.needle, c.cap, got, c.want)
		}
	}
}

// Property: an excerpt is valid UTF-8 and holds the match it is built
// around. The cut fell on byte offsets, so a line of em-dashes or
// accented letters (the corpus has both) was cut mid-rune and the MCP
// tool and `gofastr docs --grep` printed half a character; and the match
// was located in strings.ToLower(line) but sliced out of line, which
// strings.ToLower lengthens for U+0130 'İ' (2 bytes) → "i̇" (3 bytes), so
// after enough of them the window missed the match entirely.
func TestExcerptAroundCutsWholeRunesAroundTheMatch(t *testing.T) {
	cases := []struct{ name, line string }{
		{"two-byte runes, odd offset", "a" + strings.Repeat("é", 150) + "NEEDLE" + strings.Repeat("é", 150)},
		{"three-byte runes", strings.Repeat("—", 101) + "x NEEDLE y" + strings.Repeat("→", 99)},
		{"lowering lengthens the prefix", strings.Repeat("İ", 200) + "NEEDLE" + strings.Repeat("b", 300)},
	}
	for _, c := range cases {
		for cut := 20; cut <= 240; cut += 37 {
			got := excerptAround(c.line, strings.ToLower(c.line), "needle", cut)
			if !utf8.ValidString(got) {
				t.Errorf("%s, cap %d: excerpt is not valid UTF-8: %q", c.name, cut, got)
			}
			if !strings.Contains(got, "NEEDLE") {
				t.Errorf("%s, cap %d: excerpt misses the match: %q", c.name, cut, got)
			}
		}
	}
}
