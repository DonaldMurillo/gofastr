package primitives

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

//gofastr:test-reads primitives/ *.go *.md
func TestRepoRegistryIsClean(t *testing.T) {
	c, findings, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	findings = append(findings, c.Lint()...)
	for _, f := range findings {
		t.Error(f)
	}
	if len(c.Primitives) == 0 {
		t.Fatal("the repo registry holds no primitive: the check would pass on an empty tree")
	}
	stale, err := c.Stale()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range slices.Sorted(maps.Keys(stale)) {
		t.Errorf("%s is %s: run `go run ./cmd/primitives gen`", rel, stale[rel])
	}
	for _, p := range c.Primitives {
		if strings.Count(p.Summary, ". ") > 0 {
			t.Errorf("%s: summary is one sentence", p.File)
		}
	}
}
