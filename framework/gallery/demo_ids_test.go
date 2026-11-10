package gallery

import (
	"regexp"
	"strings"
	"testing"
)

var (
	demoIDRe  = regexp.MustCompile(`\sid="([^"]*)"`)
	demoRefRe = regexp.MustCompile(`\s(aria-labelledby|aria-describedby|aria-controls|aria-owns|aria-activedescendant|for|list)="([^"]*)"`)
)

// A demo is one page region: an id it writes twice sends the second
// label to the first control (the FormFrame demo drew one frame at two
// widths, both with the same field ids), and an id reference that names
// nothing in the demo is a label or description nobody hears.
func TestDemosWriteEachIDOnceAndResolveTheirReferences(t *testing.T) {
	for _, g := range Grouped() {
		for _, e := range g.Entries {
			if e.Demo == nil {
				continue
			}
			h := string(e.Demo())
			ids := map[string]int{}
			for _, m := range demoIDRe.FindAllStringSubmatch(h, -1) {
				ids[m[1]]++
			}
			for id, n := range ids {
				if n > 1 {
					t.Errorf("%s: id %q is written %d times", e.Slug, id, n)
				}
			}
			for _, m := range demoRefRe.FindAllStringSubmatch(h, -1) {
				for _, ref := range strings.Fields(m[2]) {
					if ids[ref] == 0 {
						t.Errorf("%s: %s names %q, which the demo does not hold", e.Slug, m[1], ref)
					}
				}
			}
		}
	}
}
