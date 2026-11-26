package embedcheck

import (
	"strings"
	"testing"
)

func TestEachGiveUpIsOneNote(t *testing.T) {
	findings, notes := loadAll(t, "giveups")
	if len(findings) != 0 {
		t.Fatalf("findings %+v", findings)
	}
	want := map[string]string{
		"func-field":         "built at call time",
		"iface-field":        "interface-typed",
		"named-iface-render": "interface-typed",
		"xpkg-twice":         "from another package",
		"xpkg-iface-render":  "interface-typed",
		"mixed-options":      "not a string literal",
	}
	got := map[string]int{}
	for _, n := range notes {
		got[n.Surface]++
		if !strings.Contains(n.Reason, want[n.Surface]) {
			t.Errorf("%s: reason %q lacks %q", n.Surface, n.Reason, want[n.Surface])
		}
	}
	for surface := range want {
		if got[surface] != 1 {
			t.Errorf("%s: %d notes, want 1", surface, got[surface])
		}
	}
}
