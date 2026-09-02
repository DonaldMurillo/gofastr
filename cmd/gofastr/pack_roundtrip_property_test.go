package main

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strconv"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The property this file states, end to end, over randomized hostile input:
//
//	for every authored gofastr.yml Y that decodes to B,
//	decode(encode(B)) deep-equals B.
//
// That is pack_test.go's banner invariant (parse ∘ pack ∘ generate == parse)
// reduced to the encode/decode pair alone, so a randomized corpus — not just
// the committed example files — exercises it. The authoring side uses
// strconv.Quote for hostile strings: core/yaml's parseQuoted runs
// strconv.Unquote, so Go quoting is the *input* grammar, independent of
// quoteYAMLString (the code under test). Deriving the oracle from the writer
// would make the test agree with any mutation of the writer.
//
// Map KEYS are drawn from a known-good set; key-side refusal accuracy is
// pack_yaml_roundtrip_security_test.go's job. Here every encode must SUCCEED
// and every decode must read back the exact blueprint.

var propSafeKeys = []string{
	"name", "title", "status", "data-label", "a-b", "a.b", "a b", "café", "日本語", "x1",
}

// hostileValueCorpus strings chosen to sit on the seams between core/yaml's
// scalar grammar contexts: block value, flow-list member, block list item,
// and map key. Every string that ever re-parsed as a different type,
// structure, or count belongs here.
var hostileValueCorpus = []string{
	"a:b", "a: b", "a:b:c", "http://x", "postgres://u@h/db",
	"- a", "-", "--", "?", ":", "::", "a:", ":a",
	"#c", "a #c", " x", "x ", "x\ty", "x\ny", "x\ry", "x\r\ny",
	"yes", "no", "on", "off", "null", "Null", "NULL", "~", "true", "True", "TRUE", "false",
	"0755", "1.0", "1e5", ".5", "5.", "+5", "-5", "0x1F", "1_000", "99.0",
	"[a]", "{a", "a,b", ",", "[]", "{}", "*", "&a", "!tag", "|", ">", "%d", "@x", "`x",
	"'q'", `"q"`, "60'", `60"`, "a'b", `a"b`, "a''b", "it's", `It's #1`, "x # y",
	"", "…", "café", "日本語", "a\x00b", "\x7f", "\x80\xff", "a\x80b\xff'",
}

func randHostileString(r *rand.Rand) string {
	switch r.IntN(4) {
	case 0:
		return hostileValueCorpus[r.IntN(len(hostileValueCorpus))]
	case 1:
		// Random printable ASCII.
		b := make([]byte, r.IntN(12))
		for i := range b {
			b[i] = byte(0x20 + r.IntN(0x7f-0x20))
		}
		return string(b)
	case 2:
		// Random bytes: control chars, invalid UTF-8 — everything Unquote
		// must survive via \xNN escapes.
		b := make([]byte, r.IntN(10))
		for i := range b {
			b[i] = byte(r.IntN(256))
		}
		return string(b)
	default:
		// Random runes incl. non-printable and astral planes.
		rs := make([]rune, r.IntN(8))
		for i := range rs {
			rs[i] = rune(r.IntN(0x11000))
		}
		return string(rs)
	}
}

func randSafeKey(r *rand.Rand) string {
	return propSafeKeys[r.IntN(len(propSafeKeys))]
}

// q is the authoring form for a hostile string literal.
func q(s string) string { return strconv.Quote(s) }

// randScalarLiteral returns an authored scalar (YAML text) and the exact Go
// value a correct decode must produce for it.
func randScalarLiteral(r *rand.Rand) (lit string, want any) {
	switch r.IntN(6) {
	case 0:
		s := randHostileString(r)
		return q(s), s
	case 1:
		n := int64(r.IntN(2000001) - 1000000)
		return strconv.FormatInt(n, 10), n
	case 2:
		f := []float64{0.5, -0.25, 99.0, 1e10, 1.5e-8}[r.IntN(5)]
		return formatYAMLFloat(f), f
	case 3:
		if r.IntN(2) == 0 {
			return "true", true
		}
		return "false", false
	case 4:
		return "null", nil
	default:
		s := randHostileString(r)
		return q(s), s
	}
}

// randPropsYAML emits a nested props/properties map body at the given indent
// and the Go value it must decode to. Mixes scalars, lists (flow and block,
// incl. MIXED block lists — map plus scalar items — the context where a bare
// colon-bearing scalar is cut into a map by parseList), and empty maps/lists.
func randPropsYAML(r *rand.Rand, indent int, depth int) (string, map[string]any) {
	pad := strings.Repeat(" ", indent)
	var sb strings.Builder
	out := map[string]any{}
	for range 1 + r.IntN(4) {
		key := randSafeKey(r)
		if _, dup := out[key]; dup {
			continue
		}
		switch r.IntN(7) {
		case 0: // scalar
			lit, want := randScalarLiteral(r)
			sb.WriteString(pad + key + ": " + lit + "\n")
			out[key] = want
		case 1: // flow list of scalars
			items := make([]string, 1+r.IntN(3))
			want := make([]any, len(items))
			for j := range items {
				lit, w := randScalarLiteral(r)
				items[j] = lit
				want[j] = w
			}
			sb.WriteString(pad + key + ": [" + strings.Join(items, ", ") + "]\n")
			out[key] = want
		case 2: // block list of maps (items nested +2 under their key)
			sb.WriteString(pad + key + ":\n")
			want := make([]any, 0, 2)
			for range 1 + r.IntN(2) {
				k2 := randSafeKey(r)
				lit, w := randScalarLiteral(r)
				sb.WriteString(pad + "  - " + k2 + ": " + lit + "\n")
				want = append(want, map[string]any{k2: w})
			}
			out[key] = want
		case 3: // MIXED block list: map item + scalar item (the Finding B context)
			sb.WriteString(pad + key + ":\n")
			k2 := randSafeKey(r)
			lit, w := randScalarLiteral(r)
			sb.WriteString(pad + "  - " + k2 + ": " + lit + "\n")
			sLit, sWant := randScalarLiteral(r)
			sb.WriteString(pad + "  - " + sLit + "\n")
			out[key] = []any{map[string]any{k2: w}, sWant}
		case 4: // empty map
			sb.WriteString(pad + key + ":\n")
			out[key] = map[string]any{}
		case 5: // empty list
			sb.WriteString(pad + key + ": []\n")
			out[key] = []any{}
		default: // nested map
			if depth > 0 {
				body, want := randPropsYAML(r, indent+2, depth-1)
				sb.WriteString(pad + key + ":\n")
				sb.WriteString(body)
				out[key] = want
			} else {
				lit, want := randScalarLiteral(r)
				sb.WriteString(pad + key + ": " + lit + "\n")
				out[key] = want
			}
		}
	}
	return sb.String(), out
}

// randomBlueprintYAML authors a hostile but decodable gofastr.yml.
func randomBlueprintYAML(r *rand.Rand) string {
	var sb strings.Builder
	sb.WriteString("app:\n")
	sb.WriteString("  name: Demo\n  module: example.com/demo\n")
	sb.WriteString("  description: " + q(randHostileString(r)) + "\n")
	sb.WriteString("  base_url: " + q(randHostileString(r)) + "\n")
	if r.IntN(2) == 0 {
		sb.WriteString("  theme:\n    primary: " + q(randHostileString(r)) + "\n    font_heading: " + q(randHostileString(r)) + "\n")
	}

	// Entities: one plain, one with every optional group — including BARE
	// groups (scope:/exposure:/pagination: with no children), which decode
	// to non-nil zero structs.
	sb.WriteString("entities:\n")
	sb.WriteString("  - name: posts\n    table: posts\n    fields:\n")
	sb.WriteString("      - name: title\n        type: string\n")
	if r.IntN(2) == 0 {
		sb.WriteString("        pattern: " + q(randHostileString(r)) + "\n")
	}
	if r.IntN(2) == 0 {
		lit, _ := randScalarLiteral(r)
		sb.WriteString("      - name: status\n        type: string\n        default: " + lit + "\n")
	}
	if r.IntN(2) == 0 {
		sb.WriteString("        values: [" + q(randHostileString(r)) + ", " + q(randHostileString(r)) + "]\n")
	}
	sb.WriteString("  - name: docs\n    table: docs\n")
	sb.WriteString("    scope:\n")      // bare group → &ScopeDeclaration{}
	sb.WriteString("    exposure:\n")   // bare group → &ExposureDeclaration{}
	sb.WriteString("    pagination:\n") // bare group → &PaginationDeclaration{}
	if r.IntN(2) == 0 {
		body, _ := randPropsYAML(r, 6, 1)
		sb.WriteString("    properties:\n")
		sb.WriteString(body)
	}
	sb.WriteString("    fields:\n      - name: body\n        type: text\n")

	// Seed rows: hostile keys and typed cells.
	if r.IntN(2) == 0 {
		sb.WriteString("seed:\n  - entity: posts\n    rows:\n")
		for range 1 + r.IntN(2) {
			k := randSafeKey(r)
			lit, _ := randScalarLiteral(r)
			sb.WriteString("      - " + k + ": " + lit + "\n")
		}
	}

	// Screens with block props (the other arbitrary-[]any carrier).
	if r.IntN(2) == 0 {
		sb.WriteString("screens:\n  - name: home\n    route: /\n    title: Home\n    body:\n")
		sb.WriteString("      - kind: html\n        props:\n")
		body, _ := randPropsYAML(r, 10, 1)
		sb.WriteString(body)
	}

	// Endpoints, nav, stubs.
	if r.IntN(2) == 0 {
		sb.WriteString("endpoints:\n  - name: ping\n    method: " + q(randHostileString(r)) + "\n    path: " + q(randHostileString(r)) + "\n")
	}
	if r.IntN(2) == 0 {
		sb.WriteString("nav:\n  - label: " + q(randHostileString(r)) + "\n    href: /\n")
	}
	if r.IntN(2) == 0 {
		sb.WriteString("middleware:\n  - name: logger\n    description: " + q(randHostileString(r)) + "\n")
	}
	return sb.String()
}

// TestPackEncodeDecodeRoundTripProperty is the randomized statement of the
// serializer's contract. Any silent drop, retype, or structural corruption
// in encode→decode shows up as a DeepEqual mismatch with the offending YAML
// in the failure message.
func TestPackEncodeDecodeRoundTripProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(20260901, 1))
	const iterations = 400
	for i := range iterations {
		yml := randomBlueprintYAML(r)
		b0, err := decodeBlueprintString(yml)
		if err != nil {
			t.Fatalf("iter %d: authored fixture failed to decode (generator bug): %v\n%s", i, err, yml)
		}
		out, err := encodeBlueprintYAML(b0)
		if err != nil {
			t.Fatalf("iter %d: encode refused a decodable blueprint: %v\n%s", i, err, yml)
		}
		b1, err := decodeBlueprintString(out)
		if err != nil {
			t.Fatalf("iter %d: pack emitted an unreadable file: %v\n--- authored ---\n%s--- packed ---\n%s", i, err, yml, out)
		}
		if !reflect.DeepEqual(b0, b1) {
			t.Fatalf("iter %d: decode(encode(B)) != B\n--- authored ---\n%s--- packed ---\n%s", i, yml, out)
		}
	}
}

// TestPackEmptyMapEmissionRoundTrips pins Finding A: a blueprint holding an
// empty map (which a bare authored `scope:` produces — decodeEntityScope
// returns a non-nil zero struct for a present-but-empty node) must serialize
// to something the decoder can read back. It used to emit `scope: {}`, and
// core/yaml rejects flow maps outright.
func TestPackEmptyMapEmissionRoundTrips(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "probe"},
		Entities: []framework.EntityDeclaration{{
			Name:  "posts",
			Scope: &framework.ScopeDeclaration{}, // bare `scope:` decodes to exactly this
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
			},
		}},
	}
	out, err := encodeBlueprintYAML(bp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(out, "{}") {
		t.Errorf("packed YAML contains a flow map, which core/yaml cannot re-read:\n%s", out)
	}
	b, err := decodeBlueprintString(out)
	if err != nil {
		t.Fatalf("pack emitted an unreadable file: %v\n%s", err, out)
	}
	if b.Entities[0].Scope == nil || *b.Entities[0].Scope != (framework.ScopeDeclaration{}) {
		t.Errorf("scope lost on round-trip: %#v", b.Entities[0].Scope)
	}
}

// TestPackColonScalarInMixedBlockListRoundTrips pins Finding B: a scalar
// string containing ':' (no space) as an item of a MIXED block list (map +
// scalar items) must be quoted. parseList cuts ANY block item at the first
// ':' into a map entry, so `- a:b` re-parses as the map {a: b} — silent
// structural corruption. Authored YAML reaches this via `- "a:b"` (a quoted
// list item skips parseList's map-cut on decode), and entity properties and
// block props both carry arbitrary []any.
func TestPackColonScalarInMixedBlockListRoundTrips(t *testing.T) {
	authored := `app:
  name: probe
entities:
  - name: posts
    table: posts
    properties:
      list:
        - a: b
        - "a:b"
    fields:
      - name: title
        type: string
`
	b0, err := decodeBlueprintString(authored)
	if err != nil {
		t.Fatalf("authored fixture failed to decode: %v", err)
	}
	list, ok := b0.Entities[0].Properties["list"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("fixture decoded wrong: %#v", b0.Entities[0].Properties["list"])
	}
	if s, ok := list[1].(string); !ok || s != "a:b" {
		t.Fatalf("authored second item is not the string a:b: %#v", list[1])
	}
	out, err := encodeBlueprintYAML(b0)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b1, err := decodeBlueprintString(out)
	if err != nil {
		t.Fatalf("re-decode failed: %v\n%s", err, out)
	}
	got := b1.Entities[0].Properties["list"]
	gotList, ok := got.([]any)
	if !ok || len(gotList) != 2 {
		t.Fatalf("list corrupted: %#v", got)
	}
	if s, ok := gotList[1].(string); !ok || s != "a:b" {
		t.Errorf("second item changed from string %q to %T %#v — silent structural corruption", "a:b", gotList[1], gotList[1])
	}
}

// TestPackValueContextFidelityMatrix checks every hostile value in all three
// scalar emission contexts, comparing the RE-PARSED value to the original —
// not merely that the file parses. A bare value that re-parses as a
// different string, type, count, or shape is a round-trip break even when
// the document stays readable.
func TestPackValueContextFidelityMatrix(t *testing.T) {
	for _, v := range hostileValueCorpus {
		if needsQuote(v) {
			continue // the quoted path is covered by the corpus round-trip above
		}
		// Context A: block map value.
		doc := "k: " + v + "\nz: end\n"
		if m, err := parseProbeDoc(doc); err != nil {
			t.Errorf("value %q as block value: re-parse errors: %v\ndoc:\n%s", v, err, doc)
		} else if got := scalarText(m, "k"); got != v {
			t.Errorf("value %q as block value: re-parsed as %q", v, got)
		}
		// Context B: flow list member.
		doc = "k: [" + v + ", z]\n"
		if m, err := parseProbeDoc(doc); err != nil {
			t.Errorf("value %q as flow member: re-parse errors: %v\ndoc:\n%s", v, err, doc)
		} else if list, ok := listNode(m, "k"); !ok || len(list) != 2 {
			t.Errorf("value %q as flow member: list count changed to %d\ndoc:\n%s", v, len(list), doc)
		} else if got := nodeScalarText(list[0]); got != v {
			t.Errorf("value %q as flow member: re-parsed as %q", v, got)
		}
		// Context C: block scalar item after a map item (mixed list). A
		// bare colon-bearing item is cut into a map by parseList — that is
		// precisely the hazard writeYAMLListItem's colon rule exists for,
		// pinned end-to-end by TestPackColonScalarInMixedBlockListRoundTrips.
		// Here, verify the residue: every non-colon value stays a faithful
		// scalar item.
		if !strings.Contains(v, ":") {
			doc = "lst:\n  - a: b\n  - " + v + "\n"
			if m, err := parseProbeDoc(doc); err != nil {
				t.Errorf("value %q as block item: re-parse errors: %v\ndoc:\n%s", v, err, doc)
			} else if list, ok := listNode(m, "lst"); !ok || len(list) != 2 {
				t.Errorf("value %q as block item: item count changed to %d\ndoc:\n%s", v, len(list), doc)
			} else if list[1].Kind != coreyaml.Scalar || list[1].Map != nil {
				t.Errorf("value %q as block item: re-parsed as %s, not a scalar", v, kindName(list[1]))
			} else if got := fmt.Sprint(list[1].Value); got != v {
				t.Errorf("value %q as block item: re-parsed as %q", v, got)
			}
		}
	}
}

func scalarText(m map[string]*coreyaml.Node, key string) string {
	n, ok := m[key]
	if !ok || n.Kind != coreyaml.Scalar {
		return "\x00missing:" + key
	}
	return fmt.Sprint(n.Value)
}

func listNode(m map[string]*coreyaml.Node, key string) ([]*coreyaml.Node, bool) {
	n, ok := m[key]
	if !ok || n.Kind != coreyaml.List {
		return nil, false
	}
	return n.List, true
}

func nodeScalarText(n *coreyaml.Node) string {
	if n.Kind != coreyaml.Scalar {
		return "\x00notscalar"
	}
	return fmt.Sprint(n.Value)
}

func kindName(n *coreyaml.Node) string {
	switch n.Kind {
	case coreyaml.Scalar:
		return "scalar"
	case coreyaml.List:
		return "list"
	case coreyaml.Map:
		return "map"
	}
	return "other"
}

func parseProbeDoc(doc string) (map[string]*coreyaml.Node, error) {
	node, err := coreyaml.Parse(doc)
	if err != nil {
		return nil, err
	}
	return node.Map, nil
}
