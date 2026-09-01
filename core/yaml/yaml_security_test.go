package yaml

import (
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
)

// inlineBomb builds a one-line document whose value is n nested inline
// lists: `a: [[[...]]]`.
func inlineBomb(n int) string {
	return "a: " + strings.Repeat("[", n) + strings.Repeat("]", n)
}

// Property: nesting depth of ANY kind is bounded at parse time. The
// block-only cap (maxNestingDepth, consulted solely by parseBlock at
// yaml.go:116) is bypassed by the inline-list path: parseScalar recurses
// into parseInlineList on a '[' prefix (yaml.go:255-256) and
// parseInlineList calls parseScalar per comma part (yaml.go:302-303) —
// mutual recursion that never consults p.depth. Nested inline lists are
// semantically rejected only AFTER the full descent (the Kind check at
// yaml.go:307), so an input-controlled number of frames is spent before
// any error surfaces. Remote attacker-controlled YAML reaches Parse
// verbatim via `gofastr generate cli --from <URL>`
// (cmd/gofastr/generate_cli_openapi.go:168), so this is network-
// reachable, not local-file-only.
//
// Proven in a subprocess with a 1 MiB stack, mirroring the block-nesting
// pin in depth_security_test.go: today the unbounded recursion blows the
// capped stack and crashes the child; with a depth guard on the inline
// path the child exits 0 with a parse error. Fixture is 50,000 bracket
// pairs (~100 KB); today's crash occurs a few thousand frames in, so
// the test's own cost stays under a couple of seconds.
func TestYAML_InlineListNoStackExhaust(t *testing.T) {
	if os.Getenv("YAML_INLINE_SUBPROCESS") == "1" {
		debug.SetMaxStack(1 << 20)
		_, _ = Parse(inlineBomb(50000))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestYAML_InlineListNoStackExhaust$")
	cmd.Env = append(os.Environ(), "YAML_INLINE_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		head := out
		if len(head) > 4000 {
			head = head[:4000]
		}
		t.Fatalf("SECURITY: [yaml] Parse of a 50000-deep inline list crashed the subprocess (inline-list recursion unbounded, bypasses maxNestingDepth): %v\n%s", err, head)
	}
}

// False-positive guard: a flat inline list keeps parsing as a List of
// scalars. A depth guard must sit on the recursion, not on the feature.
func TestYAML_FlatInlineListStillParses(t *testing.T) {
	node, err := Parse("a: [1, 2, 3]\n")
	if err != nil {
		t.Fatalf("flat inline list must parse: %v", err)
	}
	inner := node.Map["a"]
	if inner == nil || inner.Kind != List || len(inner.List) != 3 {
		t.Errorf("flat inline list shape regressed: %#v", inner)
	}
}
