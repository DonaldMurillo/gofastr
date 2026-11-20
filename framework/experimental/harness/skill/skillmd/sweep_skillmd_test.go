package skillmd

import (
	"strings"
	"testing"
)

func TestParseBytesEnforcesSpecLimits(t *testing.T) {
	doc := func(name, desc string) []byte {
		return []byte("---\nname: " + name + "\ndescription: " + desc + "\n---\nbody")
	}
	cases := map[string][]byte{
		"name over 64":          doc(strings.Repeat("a", 65), "d"),
		"description missing":   doc("ok", ""),
		"description over 1024": doc("ok", strings.Repeat("d", 1025)),
		"no frontmatter":        []byte("xxxname: ok\ndescription: d\n---\nbody"),
	}
	for label, src := range cases {
		if _, err := ParseBytes(src); err == nil {
			t.Errorf("%s: ParseBytes accepted it", label)
		}
	}
	if _, err := ParseBytes(doc(strings.Repeat("a", 64), strings.Repeat("d", 1024))); err != nil {
		t.Errorf("limits are inclusive: %v", err)
	}
}
