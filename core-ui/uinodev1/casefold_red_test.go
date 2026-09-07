//go:build red

package uinodev1

import (
	"fmt"
	"testing"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2 (family
// enumeration; tier T2).
// Property: a ui.node.v1 document may not contain two keys that fold onto one
// struct field under ASCII case folding. rejectDuplicateKeys
// (validate.go:298-362) already refuses EXACT duplicate keys anywhere in the
// document, with the stated rationale that stdlib's silent last-key-wins is a
// smuggling vector — but its seen-map compares keys byte-exactly, so
// {"Component":...,"component":...} and {"Src":...,"src":...} walk past it,
// and Go's case-insensitive struct-tag matching then folds the pair onto one
// field, last wins, at every node level and inside every props object. The
// strict family rule (handler.DecodeStrict: "no key may repeat, and no two
// keys may fold to the same name under ASCII case folding") closes exactly
// this seam everywhere else.
// Surfaces: core-ui/uinodev1/validate.go::rejectDuplicateKeys :298-362
// (exact-match seen-map only) feeding decodeNode's shadow struct
// (validate.go:120-130, field `component`) and the per-component prop
// decoders (registry.go strictDecode, e.g. ImageProps `src`).
// Finding: a third-party module tree with a case-folded component key
// validates clean and renders as the LAST spelling while any first-
// occurrence reader of the JSON (proxy, logger, review tool) saw the first;
// the same fold inside image props hides a scheme-relative src
// ("//evil.example/x") behind a benign one ("/ok.jpg") that the URL guard
// then blesses.
// Fix direction: extend the duplicate-key walk to flag two keys in the same
// object that are equal under ASCII case folding (the CheckObjectKeys
// fold rule), so the ambiguity is refused before any structural decode.

func TestValidateRejectsCaseFoldedKeysRed(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{
			name: "root component folds onto the component field",
			doc:  `{"Component":"paragraph","component":"text","props":{"text":"folded root"}}`,
		},
		{
			name: "child node component folds one level down",
			doc:  `{"component":"stack","props":{},"children":[{"Component":"paragraph","component":"text","props":{"text":"folded child"}}]}`,
		},
		{
			name: "image src folds inside props, hiding a scheme-relative URL",
			doc:  `{"component":"image","props":{"src":"//evil.example/x","Src":"/ok.jpg","alt":"folded props"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := Validate([]byte(tc.doc), DefaultLimits())
			if err != nil {
				return // secure: the ambiguity was refused
			}
			detail := ""
			if ip, ok := tree.Root.Props.(ImageProps); ok {
				detail = fmt.Sprintf(", decoded src=%q", ip.Src)
			}
			t.Errorf("SECURITY: [uinodev1-casefold] Validate accepted a tree with case-folded keys (%s): decoded component=%q%s — the exact-match duplicate walk lets case-variant spellings of one field through and stdlib json folds them last-wins; refuse the ambiguity", tc.name, tree.Root.Component, detail)
		})
	}

	// GREEN-guard: canonical trees still validate, so the fold check cannot
	// simply refuse everything.
	for _, doc := range []string{
		`{"component":"paragraph","props":{"text":"clean tree"}}`,
		`{"component":"image","props":{"src":"/ok.jpg","alt":"clean alt text"}}`,
	} {
		if _, err := Validate([]byte(doc), DefaultLimits()); err != nil {
			t.Fatalf("setup broken: canonical tree must validate: %s: %v", doc, err)
		}
	}
}
