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

// Property: anchor, alias, and tag syntax is rejected at every scalar
// position a value can occupy — parseScalar is shared by map values,
// nested map values, bare list items, list-item map values, and inline
// list elements, so a `&`/`*`/`!!` prefix must fail everywhere, not just
// at the top level. The parser resolves nothing, so this is fail-closed
// syntax rejection (no billion-laughs surface can even open).
func TestYAMLAnchorAliasRejectedAtValueSurfaces(t *testing.T) {
	docs := map[string]string{
		"anchor as map value":     "a: &anch 1\n",
		"alias as map value":      "a: *anch\n",
		"anchor in nested map":    "outer:\n  inner: &x 1\n",
		"alias as list item":      "items:\n  - *x\n",
		"anchor in list-item map": "items:\n  - k: &x 1\n",
		"anchor in inline list":   "a: [&x 1, b]\n",
		"alias in inline list":    "a: [b, *x]\n",
		"tag as map value":        "a: !!str 1\n",
		"tag in nested map":       "outer:\n  inner: !!int 7\n",
		"anchor at deep indent":   "a:\n  b:\n    c:\n      d: &deep 1\n",
	}
	for name, doc := range docs {
		_, err := Parse(doc)
		if err == nil {
			t.Errorf("SECURITY: [yaml] anchor/alias/tag syntax accepted at surface %q: %q. Attack: unresolved alias machinery is an unbounded-recursion/exponential-expansion surface the parser refuses by contract.", name, doc)
		}
	}
}

// Property: the anchor rejection and the nesting cap compose — an
// anchor smuggled to the bottom of a deep-but-legal document is
// rejected as an anchor (not parsed as a value), and the same shape one
// level past the cap is rejected by the depth guard. Neither check may
// silently admit what the other was supposed to catch.
func TestYAMLAnchorGuardsComposeWithDepth(t *testing.T) {
	deepAnchor := func(levels int) string {
		var sb strings.Builder
		for i := range levels {
			sb.WriteString(strings.Repeat(" ", i))
			sb.WriteString("k:\n")
		}
		sb.WriteString(strings.Repeat(" ", levels))
		sb.WriteString("v: &boom 1\n")
		return sb.String()
	}

	under := deepAnchor(100) // well under the 128 cap
	if _, err := Parse(under); err == nil {
		t.Error("SECURITY: [yaml] anchor accepted at the bottom of a 100-deep document (depth guard masked the anchor guard)")
	}
	over := deepAnchor(200) // past the cap
	if _, err := Parse(over); err == nil {
		t.Error("SECURITY: [yaml] 200-deep document with an anchor at the bottom parsed cleanly (both guards missed)")
	}
}

// Property: a mapping key that differs from its bare spelling only by
// quoting or anchor/tag decoration must not parse as a DISTINCT key.
// parseScalar rejects `&`, `*`, and `!!` prefixes at every VALUE
// position (pinned above), but parseMapSeeded/parseList never unquote
// or syntax-check the KEY side of `key: value`, so `"auth"` (quotes
// retained verbatim in the key), `&auth`, and `auth` are three
// different map keys. That defeats the duplicate-key fail-closed guard
// this package exists to enforce (see security_test.go): a hostile or
// stale line smuggled in as `"auth": ...` shadows nothing, conflicts
// with nothing, and a human reading the file sees "auth" configured.
// Surfaces: top-level map keys, nested map keys, list-item first keys
// (which also skip the `[]{}` flow-character check the plain map path
// enforces at yaml.go:184).
func TestYAMLQuotedKeysEvadeDuplicateGuard(t *testing.T) {
	docs := map[string]string{
		"quoted twin at top level": "\"title\": second\ntitle: first\n",
		"single-quoted twin":       "'title': second\ntitle: first\n",
		"anchor-decorated key":     "&title: second\ntitle: first\n",
		"tag-decorated key":        "!!str title: second\ntitle: first\n",
		"nested map keys":          "auth:\n  \"enabled\": true\n  enabled: false\n",
		"list-item continuation":   "items:\n  - a: 1\n    \"a\": 2\n",
		"list-item flow-char key":  "items:\n  - a[b]: 1\n",
	}
	for name, doc := range docs {
		if _, err := Parse(doc); err == nil {
			t.Errorf("SECURITY: [yaml] %s parsed cleanly — decorated/quoted keys are distinct map keys, so the duplicate-key guard never fires. Doc:\n%s", name, doc)
		}
	}
	// False-positive guard: ordinary distinct keys and bare keys with
	// ordinary values must keep parsing. A fix must sit on key
	// decoration/unquoting, not on rejecting maps outright.
	if _, err := Parse("title: first\nother: second\n"); err != nil {
		t.Errorf("plain distinct keys rejected: %v", err)
	}
}
