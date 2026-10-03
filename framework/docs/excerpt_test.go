package docs

import (
	"strings"
	"testing"
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
