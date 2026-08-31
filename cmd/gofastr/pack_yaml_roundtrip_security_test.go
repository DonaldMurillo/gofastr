package main

import (
	"fmt"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The invariant, stated end-to-end rather than as a predicate: pack either
// REFUSES a map key or emits YAML that reads back with the key and value
// intact — and it refuses only what it must.
//
// yamlKeyRejectReason's table checks that each branch fires for the right
// reason. This checks the property that motivates having branches at all,
// and it is the test that would have caught the quote bypass: `it's` passed
// every branch, and the emitted `it's: "It's #1"` was unreadable because the
// key's apostrophe desynced the comment scanner from the correctly-quoted
// value. No predicate-level assertion covers an interaction like that.
//
// The lists below are the oracle, deliberately NOT derived from
// yamlKeyRejectReason. Asking the code under test what it should do makes
// the test agree with any mutation of it: a first version of this called
// yamlKeyRejectReason for the expectation, and a guard that refused EVERY
// key passed. The expectation has to be stated independently or it is not
// an expectation.
var (
	// Refused because raw emission genuinely breaks the round trip, in at
	// least one of the two reachable contexts (a seed row, which is a map
	// inside a list item, and entity properties, which is a nested map).
	mustRefuseBreaking = []string{
		"", "a\nb", "a:b", "a[b", "a]b", "a{b", "a}b", "- a", "a\tb",
		" a", "a ", `"status`, "'status", "it's", `"quoted"`, "'quoted'",
		// Leading '#' is the load-bearing one: the comment eats the line
		// and the entry is SILENTLY DROPPED — the row re-parses as {}.
		// pack.go contemplates relaxing '#' to a position-exact check;
		// that relaxation must not be allowed to accept these.
		"#a", "a #b",
		"{a", "[a",
		// Quote family: the value is quoted correctly and the key's quote
		// still desyncs the comment scanner against it.
		`a"b`, "a'b",
		// Colon family: parseMap cuts at the first colon regardless of
		// context, so the key comes back truncated.
		":", ":a", "a:", "a: b",
		// Comment family, mirroring the #a/a #b pair already listed.
		"#", "a #", " #a",
	}
	// Refused conservatively: these would in fact survive raw emission
	// today, but the guard rejects them anyway because the predicate is
	// simpler than the parser's real rule. Listed separately so the
	// comment above stays true of the list above it.
	mustRefuseConservatively = []string{"a\rb", "a#b"}

	// Must survive untouched. Refusing any of these breaks real packs,
	// which is the failure mode that matters more than the injection.
	mustRoundTripKeys = []string{
		"name", "created_at", "data-label", "primary-fg", "text-muted",
		"icon", "-a", "a-b", "a.b", "a b", "Title", "id", "C", "x1",
		"a/b", "a|b", "a?b", "a!b", "a@b", "a%b", "a&b", "a*b", "a+b",
		"a=b", "a<b", "a>b", "a(b)", "café", "日本語",
	}
	// Values are hostile too: the bypass that motivated this test only
	// appeared when a quoted VALUE met a quote-bearing key on one line.
	hostileValues = []string{"open", `It's #1`, `a "b" c`, "x # y"}
)

func TestPackNeverEmitsAnUnreadableFile(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		for _, key := range append(append([]string{}, mustRefuseBreaking...), mustRefuseConservatively...) {
			for _, val := range hostileValues {
				if _, err := encodeKeyValue(key, val); err == nil {
					t.Errorf("key %q (value %q) was accepted; it must be refused", key, val)
				}
			}
		}
	})

	t.Run("round-trips", func(t *testing.T) {
		for _, key := range mustRoundTripKeys {
			for _, val := range hostileValues {
				out, err := encodeKeyValue(key, val)
				if err != nil {
					t.Errorf("legitimate key %q (value %q) was refused: %v", key, val, err)
					continue
				}
				// Readable is not enough: a writer that mangled or dropped
				// keys would still parse. Parse("") even returns an empty
				// map with a nil error, so "it parsed" passes vacuously for
				// an emit-nothing regression. Check the value came back.
				got, err := seedRowValue(out, key)
				if err != nil {
					t.Errorf("key %q with value %q: %v\n%s", key, val, err, out)
					continue
				}
				if got != val {
					t.Errorf("key %q round-tripped to %q, want %q\n%s", key, got, val, out)
				}
			}
		}
	})
}

// seedRowValue re-parses pack output and returns seed[0].rows[0][key],
// so the test observes what the writer actually preserved.
func seedRowValue(out, key string) (string, error) {
	node, err := coreyaml.Parse(out)
	if err != nil {
		return "", fmt.Errorf("core/yaml cannot read the emitted file: %v", err)
	}
	seed := node.Map["seed"]
	if seed == nil || len(seed.List) == 0 {
		return "", fmt.Errorf("emitted file has no seed entry")
	}
	rows := seed.List[0].Map["rows"]
	if rows == nil || len(rows.List) == 0 {
		return "", fmt.Errorf("emitted file has no seed rows")
	}
	row := rows.List[0]
	cell := row.Map[key]
	if cell == nil {
		return "", fmt.Errorf("key %q is absent from the re-parsed row (present keys: %v)", key, rowKeyNames(row.Map))
	}
	s, _ := cell.Value.(string)
	return s, nil
}

func rowKeyNames(m map[string]*coreyaml.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func encodeKeyValue(key, val string) (string, error) {
	return encodeBlueprintYAML(Blueprint{
		App: BlueprintApp{Name: "probe", Module: "example.com/probe"},
		Seed: []BlueprintSeedEntity{{
			Entity: "notes",
			Rows:   []map[string]any{{key: val}},
		}},
	})
}

// The two refused lists differ only in WHY, and until now that why was a
// comment. It was wrong once: a"b sat under "would survive raw emission"
// while in fact breaking in every context, so this file blessed a claim
// about the parser that the guard's own comment contradicted.
//
// This checks the classification instead of asserting it in prose: a
// breaking key must actually break raw emission for at least one value, and
// a conservative key must actually survive for all of them. Bypasses the
// guard by building the row line the writer would emit.
func TestRefusedKeyClassificationIsAccurate(t *testing.T) {
	t.Run("breaking keys really break", func(t *testing.T) {
		for _, key := range mustRefuseBreaking {
			if key == "" {
				continue // the empty key has no raw line to build
			}
			broke := false
			for _, val := range hostileValues {
				if rawRowRoundTrips(key, val) != nil {
					broke = true
					break
				}
			}
			if !broke {
				t.Errorf("key %q is listed as breaking, but raw emission round-trips "+
					"for every hostile value — it belongs in mustRefuseConservatively", key)
			}
		}
	})

	t.Run("conservative keys really survive", func(t *testing.T) {
		for _, key := range mustRefuseConservatively {
			for _, val := range hostileValues {
				if err := rawRowRoundTrips(key, val); err != nil {
					t.Errorf("key %q is listed as refused-conservatively, but raw emission "+
						"breaks with value %q (%v) — it belongs in mustRefuseBreaking",
						key, val, err)
				}
			}
		}
	})
}

// rawRowRoundTrips reports whether the raw (guard-bypassed) emission of
// key/val survives a re-parse in EITHER reachable context. A key is
// "breaking" if it fails in at least one: single-context classification
// under-reports, because [ ] { } and a leading "- " survive as a seed-row
// list item while parseMap screens them as a nested map key.
func rawRowRoundTrips(key, val string) error {
	if err := rawSeedRowRoundTrips(key, val); err != nil {
		return fmt.Errorf("seed row: %w", err)
	}
	if err := rawPropertiesRoundTrips(key, val); err != nil {
		return fmt.Errorf("entity properties: %w", err)
	}
	return nil
}

// Seed rows are a map inside a list item: "      - key: value".
func rawSeedRowRoundTrips(key, val string) error {
	doc := "seed:\n  - entity: notes\n    rows:\n      - " + key + ": " + quoteYAMLString(val) + "\n"
	node, err := coreyaml.Parse(doc)
	if err != nil {
		return err
	}
	seed := node.Map["seed"]
	if seed == nil || len(seed.List) == 0 {
		return fmt.Errorf("seed entry vanished on re-parse")
	}
	rows := seed.List[0].Map["rows"]
	if rows == nil || len(rows.List) == 0 {
		return fmt.Errorf("rows vanished on re-parse")
	}
	return cellMatches(rows.List[0].Map, key, val)
}

// Entity properties are a plain nested map: "      key: value".
func rawPropertiesRoundTrips(key, val string) error {
	doc := "entities:\n  - name: notes\n    properties:\n      " + key + ": " + quoteYAMLString(val) + "\n"
	node, err := coreyaml.Parse(doc)
	if err != nil {
		return err
	}
	ents := node.Map["entities"]
	if ents == nil || len(ents.List) == 0 {
		return fmt.Errorf("entity vanished on re-parse")
	}
	props := ents.List[0].Map["properties"]
	if props == nil {
		return fmt.Errorf("properties vanished on re-parse")
	}
	return cellMatches(props.Map, key, val)
}

func cellMatches(m map[string]*coreyaml.Node, key, val string) error {
	cell := m[key]
	if cell == nil {
		return fmt.Errorf("key absent on re-parse (present: %v)", rowKeyNames(m))
	}
	if got, _ := cell.Value.(string); got != val {
		return fmt.Errorf("value came back %q, want %q", got, val)
	}
	return nil
}

// The value-side mirror of the key-side story above, in the one context where
// a quote-bearing value fails SILENTLY: the flow list. needsQuote used to
// treat a quote character as a first-character-only indicator, so an enum
// value like 60' was emitted bare inside `values: [...]`. splitInline
// (core/yaml) tracks quote state across the whole list, so the apostrophe
// OPENED a quoted region that swallowed the separator: [60', 90'] re-parsed
// as ONE member "60', 90'", and a lone [60'] failed to parse at all (#323).
//
// Values, unlike keys, CAN be quoted, so the fix quotes rather than refuses.
// The oracle is the input list itself — stated here, not derived from
// needsQuote, for the same reason as the key lists above.
var mustRoundTripEnumValues = []string{
	"60'",          // the lone foot mark: unterminated quote when bare
	`90'`,          // a second quote-bearing member: the silent-merge pair
	`8"`,           // inch mark
	`5'11"`,        // both quote kinds in one value
	"open,closed",  // bare comma is an item separator
	`six'in," out`, // comma AND quotes together
	"plain",        // boring neighbor keeps the list multi-member on one line
}

func TestPackEnumValuesRoundTripThroughFlowList(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "probe"},
		Entities: []framework.EntityDeclaration{{
			Name: "windows",
			Fields: []framework.FieldDeclaration{{
				Name:   "width",
				Type:   "enum",
				Values: mustRoundTripEnumValues,
			}},
		}},
	}
	yml, err := encodeBlueprintYAML(bp)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	// Pin the context: the silent merge needs two members on ONE line, which
	// only the flow-list emission provides. If the writer ever moves enum
	// values to one-item-per-line, this guard — and the hazard it pins —
	// moves with it.
	if !strings.Contains(yml, "values: [") {
		t.Errorf("enum values are not emitted as a flow list; the merge hazard this test pins lives only there:\n%s", yml)
	}
	b, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if len(b.Entities) == 0 || len(b.Entities[0].Fields) == 0 {
		t.Fatalf("entity or field vanished on re-parse.\n--- yaml ---\n%s", yml)
	}
	got := b.Entities[0].Fields[0].Values
	if len(got) != len(mustRoundTripEnumValues) {
		t.Fatalf("enum values came back as %d members, want %d — a quote swallowed the separator:\n%v\n--- yaml ---\n%s",
			len(got), len(mustRoundTripEnumValues), got, yml)
	}
	for i, want := range mustRoundTripEnumValues {
		if got[i] != want {
			t.Errorf("enum value[%d] came back %q, want %q\n--- yaml ---\n%s", i, got[i], want, yml)
		}
	}
}
