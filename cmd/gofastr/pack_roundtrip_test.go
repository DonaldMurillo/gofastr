package main

import (
	"reflect"
	"testing"
)

// Two serializer defects found by the #362 audit, both stated as the property
// the serializer's own banner claims (pack.go: parse ∘ pack is the identity
// on decodeBlueprint output) rather than as predicates on the writer:
//
//  1. writeYAMLEntry emitted `key: {}` for an empty map, and core/yaml
//     rejects flow mappings outright (parseScalar → flowMapError), so a
//     blueprint that legitimately decodes to an empty grouped struct — an
//     authored bare `scope:`, `pagination:`, `exposure:`, `access:`, or an
//     empty map inside properties/seed rows — packed to a file
//     decodeBlueprintString could not read at all.
//  2. needsQuote("a:b") is false, which is correct for block VALUES (the
//     parser only rejects ": " there), but parseList cuts ANY unquoted block
//     list item at the first ':' into a map entry. Two different rules, and
//     the writer applied the value rule where the list rule governs: a
//     colon-bearing string in a mixed block list (the realistic payload is a
//     URL) re-packed as `- a: b` and re-read as a map the source never
//     declared.
//
// The comparison is reflect.DeepEqual over the WHOLE Blueprint, so a field
// the writer silently drops fails the comparison rather than being skipped
// by it — a comparison that only walks fields both sides have cannot see a
// dropped one.

// TestPack_EmptyMapsRoundTrip: every grouped decoder returns a non-nil zero
// struct for a present-but-empty node, so each shape below is authored YAML
// that decodes cleanly. The serializer must emit the exact inverse — a bare
// `key:` line, which re-parses as an empty map — instead of `key: {}`.
func TestPack_EmptyMapsRoundTrip(t *testing.T) {
	yml := `app:
  name: EmptyMaps
  module: example.com/emptymaps
entities:
  - name: posts
    scope:
    pagination:
    exposure:
      access:
    fields:
      - name: title
        type: string
    properties:
      meta:
seed:
  - entity: posts
    rows:
      - title: first
        meta:
      -
      - title: second
`
	a, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// The fixture must actually exercise the fix, or a green run proves
	// nothing: every empty shape below is the non-nil-zero-struct form the
	// grouped decoders produce for a present-but-empty node.
	if len(a.Entities) != 1 {
		t.Fatalf("fixture does not exercise the fix: %+v", a.Entities)
	}
	e := a.Entities[0]
	if e.Scope == nil || e.Pagination == nil || e.Exposure == nil || e.Exposure.Access == nil {
		t.Fatalf("fixture does not exercise the fix: bare group node decoded to nil — scope=%v pagination=%v exposure=%v", e.Scope, e.Pagination, e.Exposure)
	}
	if s, ok := e.Properties["meta"].(map[string]any); !ok || len(s) != 0 {
		t.Fatalf("fixture does not exercise the fix: properties.meta = %#v, want empty non-nil map", e.Properties["meta"])
	}
	rows := a.Seed[0].Rows
	if m, ok := rows[0]["meta"].(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("fixture does not exercise the fix: rows[0].meta = %#v, want empty non-nil map", rows[0]["meta"])
	}
	if len(rows) != 3 || len(rows[1]) != 0 {
		t.Fatalf("fixture does not exercise the fix: rows = %#v, want a fully empty row in the middle", rows)
	}

	out, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	b, err := decodeBlueprintString(out)
	if err != nil {
		t.Fatalf("pack emitted an unreadable file: %v\n--- yaml ---\n%s", err, out)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("empty-map round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), out)
	}
}

// TestPack_ColonScalarsInMixedListsRoundTrip: a quoted item `- "a:b"` decodes
// as the string "a:b" (parseList skips the map-cut for a leading quote). The
// serializer must re-quote it — in the block-list context ANY colon is the
// map-cut delimiter, unlike block values where only ": " is — or the item
// re-parses as {a: b} and two different strings become the same map.
func TestPack_ColonScalarsInMixedListsRoundTrip(t *testing.T) {
	yml := `app:
  name: Colons
  module: example.com/colons
entities:
  - name: posts
    fields:
      - name: title
        type: string
    properties:
      links:
        - "postgres://u@h/db"
        - "a:b"
        - label: home
screens:
  - name: home
    route: /
    body:
      - kind: card
        props:
          links:
            - "https://cdn.example.net/img.png"
            - alt: hero
`
	a, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Oracle stated independently of the fix: the quoted items must have
	// decoded as STRINGS, or the mismatch below could be blamed on the
	// fixture instead of the writer.
	if len(a.Entities) != 1 {
		t.Fatalf("fixture does not exercise the fix: %+v", a.Entities)
	}
	links, ok := a.Entities[0].Properties["links"].([]any)
	if !ok || len(links) != 3 {
		t.Fatalf("fixture does not exercise the fix: properties.links = %#v", a.Entities[0].Properties["links"])
	}
	for i, want := range []string{"postgres://u@h/db", "a:b"} {
		if got, ok := links[i].(string); !ok || got != want {
			t.Fatalf("fixture does not exercise the fix: properties.links[%d] = %#v, want the string %q", i, links[i], want)
		}
	}
	blocks := a.Screens[0].Body
	if len(blocks) != 1 {
		t.Fatalf("fixture does not exercise the fix: screens[0].body = %+v", blocks)
	}
	if _, ok := blocks[0].Props["links"].([]any); !ok {
		t.Fatalf("fixture does not exercise the fix: block props.links = %#v", blocks[0].Props["links"])
	}

	out, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	b, err := decodeBlueprintString(out)
	if err != nil {
		t.Fatalf("pack emitted an unreadable file: %v\n--- yaml ---\n%s", err, out)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("colon-scalar round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), out)
	}
}
