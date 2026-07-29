package embedcheck

import (
	"os"
	"testing"
)

// ROUND-2 REVIEW CENSUS (not an assertion). Prints the unresolved notes every
// existing fixture emits, so the blast radius of "a note now fails the build"
// is visible. Run with -v.
func TestNoteCensus(t *testing.T) {
	entries, err := os.ReadDir("testdata/src")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "loaderror" || name == "findinganderror" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			findings, notes := loadAll(t, name)
			t.Logf("%s: findings=%d notes=%d", name, len(findings), len(notes))
			for _, n := range notes {
				t.Logf("  NOTE %s", n.Reason)
			}
		})
	}
}
