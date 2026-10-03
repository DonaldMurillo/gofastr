package headless

// One golden for every component, generated from the specs.
//
// A class map's own snapshots pin the styled markup a page gets. This pins
// the other half: the structure, at the nil Classes, which is where every
// accessibility guarantee actually lives. Until now that half was
// asserted only where somebody had written an assertion for it, so a
// change to a component nobody had thought about — an attribute
// dropped, a wrapper added, a role quietly changed — passed in silence
// as long as it broke none of the named checks.
//
// It costs nothing to maintain, because the fixtures already exist and
// are already justified. Regenerate with GOFASTR_UPDATE_GOLDEN=1 after an
// intentional change, and read the diff: every line of it is a change
// to what a screen reader will be told.

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGolden(t *testing.T) {
	var b strings.Builder
	for _, sp := range Specs() {
		for _, c := range sp.Cases(Kit{}) {
			b.WriteString("=== " + sp.Name + " / " + c.Name + " ===\n")
			b.WriteString(c.Why + "\n")
			b.WriteString(string(c.HTML))
			b.WriteString("\n\n")
		}
	}
	got := b.String()

	const path = "testdata/spec_golden.txt"
	if os.Getenv("GOFASTR_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing golden: %v", err)
		}
		return
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v (run with GOFASTR_UPDATE_GOLDEN=1 to create)", err)
	}
	if string(golden) != got {
		t.Fatalf("the structure of one or more components changed.\n"+
			"Every difference below is a change to what assistive tech is told.\n"+
			"If it is intended, regenerate with GOFASTR_UPDATE_GOLDEN=1 and read the diff.\n\n%s",
			firstDifference(string(golden), got))
	}
}

// firstDifference reports the block that changed rather than dumping
// both files: a diff of four hundred fixtures is a diff nobody reads.
func firstDifference(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			return "line " + itoa(i+1) + ":\n  want: " + w[i] + "\n   got: " + g[i]
		}
	}
	if len(w) != len(g) {
		return "the fixtures gained or lost lines: " + itoa(len(w)) + " -> " + itoa(len(g))
	}
	return "(no line differs; whitespace?)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
