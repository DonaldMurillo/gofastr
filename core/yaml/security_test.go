package yaml

import (
	"strings"
	"testing"
)

// TestYAML_DuplicateKeysRejected pins that a document mapping the same key
// twice is a parse error, not a silent last-wins.
//
// parseMap wrote out.Map[key] = node with no duplicate detection, so in
//
//	app:
//	  auth:
//	    enabled: false
//	    enabled: true
//
// the effective value is `true` while a human reviewing the file top-to-
// bottom reads the first occurrence, `false`. Every mainstream YAML parser
// (yaml.v3 "already defined", Go's sigs.k8s.io/yaml via JSON semantics)
// rejects duplicate mapping keys precisely because silent last-wins lets a
// stale or hostile copy-paste line override the value a reviewer believes
// is in force. For this parser's consumers — gofastr blueprints, codegen
// configs, kiln freeze output, contracts config — the parsed file is the
// security config (auth.enabled is access-gating, cf. protectiveBool), so
// ambiguity must fail closed at the parser, not be adjudicated by map
// insertion order.
//
// Same key, different values, is the clear case. Same key, same value, is
// still ambiguous (it can hide a meaningful later edit), but this test pins
// only the clear case.
//
// The list-item case covers the second silent-merge path: parseList builds
// the first key of a "- key: value" item itself and then merges the
// indented continuation lines with maps.Copy, which also silently
// overwrote earlier keys.
func TestYAML_DuplicateKeysRejected(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"top level, different values", "auth: false\nauth: true\n"},
		{"nested map, different values", "app:\n  auth:\n    enabled: false\n    enabled: true\n"},
		{"duplicate key with map values", "dev:\n  a: 1\ndev:\n  a: 2\n"},
		{"duplicate key inside one list item", "items:\n  - a: 1\n    a: 2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.doc)
			if err == nil {
				t.Fatalf("SECURITY: [yaml] Parse accepted duplicate mapping keys (silent last-wins): %q", c.doc)
			}
		})
	}

	// Distinct keys must still parse.
	if _, err := Parse("a: 1\nb: 2\n"); err != nil {
		t.Fatalf("distinct keys must parse: %v", err)
	}
	// Repeated keys in DIFFERENT list items are fine; only keys that
	// collapse into one mapping are ambiguous.
	if _, err := Parse("items:\n  - a: 1\n  - a: 2\n"); err != nil {
		t.Fatalf("same key in sibling list items must parse: %v", err)
	}
}

// TestYAML_DuplicateKeyErrorNamesKeyAndLines pins the error contract: the
// message must name the duplicated key and both defining lines, so a reader
// can jump straight to the conflict instead of grepping. Before the guard
// existed this shape parsed successfully with last-wins semantics (the
// demonstration half of the finding above).
func TestYAML_DuplicateKeyErrorNamesKeyAndLines(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"flat", "auth: false\nauth: true\n", `yaml:2:1: duplicate mapping key "auth" (first defined at line 1)`},
		{"nested", "app:\n  auth:\n    enabled: false\n    enabled: true\n", `yaml:4:5: duplicate mapping key "enabled" (first defined at line 3)`},
		// A list item defines its first key on the "- key: value" line, and
		// the continuation lines are parsed as a separate map. Without
		// seeding that map, the first repeat among the continuations was
		// reported as the first definition — line 3 here, when `a` was
		// defined on line 2. The reported line is the point of the error.
		{"list item then two continuations", "items:\n  - a: 1\n    a: 2\n    a: 3\n", `yaml:3:5: duplicate mapping key "a" (first defined at line 2)`},
		{"list item then one continuation", "items:\n  - a: 1\n    a: 2\n", `yaml:3:5: duplicate mapping key "a" (first defined at line 2)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.doc)
			if err == nil {
				t.Fatalf("expected duplicate-key error for %q", c.doc)
			}
			if err.Error() != c.want {
				t.Fatalf("error mismatch:\n got: %s\nwant: %s", err, c.want)
			}
		})
	}

	// The list-item merge path shares the message shape.
	_, err := Parse("items:\n  - a: 1\n    a: 2\n")
	if err == nil {
		t.Fatal("expected duplicate-key error for list item continuation")
	}
	for _, want := range []string{`duplicate mapping key "a"`, "first defined at line 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("list-item error must contain %q, got: %s", want, err)
		}
	}
}

// TestYAML_YAML11BooleansStayStrings pins the YAML 1.2 core-schema rule the
// blueprint layer relies on: yes/no/on/off/y/n are STRINGS, not booleans.
// Strict consumers (strictBoolValue, protectiveBool, requiredBoolValue)
// reject them loudly instead of coercing; a lax reader that defaults
// non-bool to false would turn `auth: yes` fail-open. This test holds the
// parser to the contract so a future "convenience" coercion cannot land
// silently.
func TestYAML_YAML11BooleansStayStrings(t *testing.T) {
	for _, raw := range []string{"yes", "no", "on", "off", "y", "n", "Yes", "NO", "TRUE", "True", "false", "False"} {
		node, err := Parse("k: " + raw + "\n")
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		v := node.Map["k"].Value
		switch raw {
		case "TRUE", "True", "false", "False":
			if _, ok := v.(bool); !ok {
				t.Errorf("%q should decode as bool, got %T(%v)", raw, v, v)
			}
		default:
			if s, ok := v.(string); !ok || s != raw {
				t.Errorf("%q should stay a string (YAML 1.2 core schema), got %T(%v)", raw, v, v)
			}
		}
	}
}
