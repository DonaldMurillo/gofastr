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

// agentsMDMaxLines is the guide/stays-short limit.
const agentsMDMaxLines = 60

//gofastr:test-reads /AGENTS.md
func TestAgentsMDStaysShort(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(guide), "\n")
	if len(guide) > 0 && guide[len(guide)-1] != '\n' {
		lines++
	}
	if lines > agentsMDMaxLines {
		t.Errorf("AGENTS.md is %d lines, over the %d-line limit (guide/stays-short): move the new rule into the primitive it governs, or into docs/contributing.md when no primitive can own it", lines, agentsMDMaxLines)
	}
}

//gofastr:test-reads /CLAUDE.md
func TestClaudeMDOnlyImportsAgentsMD(t *testing.T) {
	got, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "@AGENTS.md" {
		t.Errorf("CLAUDE.md holds %q, want only the line @AGENTS.md (guide/claude-md-only-imports): put the text in AGENTS.md, which every agent reads", got)
	}
}

// ruleRef is the primitive/invariant pair that ends a numbered rule,
// whatever separates the rule from it. Paths earlier in the rule, such
// as `framework/ui`, are not references.
var ruleRef = regexp.MustCompile("(?m)^\\d+\\. .*`([a-z0-9.-]+)/([a-z0-9-]+)`\\.?$")

func ruleRefs(guide string) [][2]string {
	var refs [][2]string
	for _, m := range ruleRef.FindAllStringSubmatch(guide, -1) {
		refs = append(refs, [2]string{m[1], m[2]})
	}
	return refs
}

func TestRuleRefsIgnoreTheSeparator(t *testing.T) {
	got := ruleRefs("1. A rule: `a/b-c`.\n2. Another rule \u2014 `ui.x/not-a-rule`.\n3. Survey `framework/ui` first.\n- not numbered `c/d`\n")
	want := [][2]string{{"a", "b-c"}, {"ui.x", "not-a-rule"}}
	if !slices.Equal(got, want) {
		t.Errorf("ruleRefs = %v, want %v", got, want)
	}
}

//gofastr:test-reads /AGENTS.md
func TestAgentsMDNamesRealRules(t *testing.T) {
	root := filepath.Join("..", "..")
	guide, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	named := 0
	for _, ref := range ruleRefs(string(guide)) {
		named++
		p := c.Primitive(ref[0])
		if p == nil {
			t.Errorf("AGENTS.md names %s/%s, and there is no primitive %s", ref[0], ref[1], ref[0])
			continue
		}
		if c.Invariant(p, ref[1]) == nil {
			t.Errorf("AGENTS.md names %s/%s, which is not an invariant of %s", ref[0], ref[1], ref[0])
		}
	}
	if named == 0 {
		t.Fatal("AGENTS.md names no registry invariant: the hard rules lost their pointers")
	}
}
