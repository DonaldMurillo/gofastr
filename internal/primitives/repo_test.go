package primitives

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var sentenceBreak = regexp.MustCompile(`[.!?]\s+\S`)

//gofastr:test-reads primitives/ *.go *.md
func TestRepoRegistryIsClean(t *testing.T) {
	c, findings, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
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
		if sentenceBreak.MatchString(p.Summary) {
			t.Errorf("%s: summary must be one sentence, got %q", p.File, p.Summary)
		}
	}
}

// claudeMDMaxLines is the guide/stays-short limit.
const claudeMDMaxLines = 60

//gofastr:test-reads /CLAUDE.md
func TestClaudeMDStaysShort(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(guide), "\n")
	if len(guide) > 0 && guide[len(guide)-1] != '\n' {
		lines++
	}
	if lines > claudeMDMaxLines {
		t.Errorf("CLAUDE.md is %d lines, over the %d-line limit (guide/stays-short): move the new rule into the primitive it governs, or into docs/contributing.md when no primitive can own it", lines, claudeMDMaxLines)
	}
}

//gofastr:test-reads /CLAUDE.md
func TestClaudeMDNamesRealRules(t *testing.T) {
	root := filepath.Join("..", "..")
	guide, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	rule := regexp.MustCompile("(?m)^\\d+\\. .*: `([a-z0-9.-]+)/([a-z0-9-]+)`")
	named := 0
	for _, m := range rule.FindAllStringSubmatch(string(guide), -1) {
		named++
		p := c.Primitive(m[1])
		if p == nil {
			t.Errorf("CLAUDE.md names %s/%s, and there is no primitive %s", m[1], m[2], m[1])
			continue
		}
		if c.Invariant(p, m[2]) == nil {
			t.Errorf("CLAUDE.md names %s/%s, which is not an invariant of %s", m[1], m[2], m[1])
		}
	}
	if named == 0 {
		t.Fatal("CLAUDE.md names no registry invariant: the hard rules lost their pointers")
	}
}
