package yaml

import (
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
)

// nestedYAML builds a YAML document with n levels of map nesting, one
// space of indent per level. Each level is a single `k:` line, so the
// recursion depth of the parser equals n.
func nestedYAML(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString(strings.Repeat(" ", i))
		sb.WriteString("k:\n")
	}
	sb.WriteString(strings.Repeat(" ", n))
	sb.WriteString("v: 1\n")
	return sb.String()
}

// TestYAML_DeepNestingRejected verifies that nesting deeper than the cap
// is rejected with an error. Before the fix there was no recursion-depth
// cap, so this depth parsed successfully (nil error). An unbounded
// recursion attacker could drive stack exhaustion.
func TestYAML_DeepNestingRejected(t *testing.T) {
	_, err := Parse(nestedYAML(200))
	if err == nil {
		t.Fatal("SECURITY: [yaml] Parse accepted 200-level nesting with no depth cap. Attack: unbounded recursion → stack exhaustion / DoS.")
	}
}

// TestYAML_ShallowNestingStillParses guards against a false positive:
// reasonable nesting (well under the cap) must still parse fine.
func TestYAML_ShallowNestingStillParses(t *testing.T) {
	node, err := Parse(nestedYAML(50))
	if err != nil {
		t.Fatalf("50-level nesting should parse cleanly, got error: %v", err)
	}
	if node == nil || node.Kind != Map {
		t.Fatalf("expected a non-nil Map root, got %#v", node)
	}
}

// TestYAML_DeepNestingNoStackExhaust proves the cap actually prevents
// stack exhaustion, not merely returns an error at a depth the stack could
// already survive. It runs the parser on deep nesting in a subprocess whose
// goroutine stack is capped at 1 MiB; without a depth cap the recursion
// blows the capped stack and the subprocess crashes (non-zero exit), with
// the cap it exits 0.
func TestYAML_DeepNestingNoStackExhaust(t *testing.T) {
	if os.Getenv("YAML_DEEP_SUBPROCESS") == "1" {
		// 1 MiB stack cap: ~3000 frames of parseBlock+parseMap (≈416 B
		// each) overflow it without the depth cap, but 128-deep
		// recursion (≈53 KiB) is well under it once the cap is in.
		debug.SetMaxStack(1 << 20)
		_, _ = Parse(nestedYAML(3000))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestYAML_DeepNestingNoStackExhaust$")
	cmd.Env = append(os.Environ(), "YAML_DEEP_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [yaml] Parse of deep nesting crashed the subprocess (no depth cap → stack exhaustion): %v\n%s", err, out)
	}
}
