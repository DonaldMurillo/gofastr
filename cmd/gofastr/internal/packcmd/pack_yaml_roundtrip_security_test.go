package packcmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
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
	mustRefuseKeys = []string{
		"", "a\nb", "a\rb", "a\tb", " a", "a ", " #a",
		"a[b", "a]b", "a{b", "a}b", "{a", "[a",
	}
	// These keys need quotes in at least one writer context.
	mustQuoteKeys = []string{
		"- a", `"status`, "'status", `"quoted"`, "'quoted'",
		"#a", "a #b", "a: b", "#", "a #", "&a", "*a", "!a",
	}
	// These keys also round-trip when emitted bare.
	mustRoundTripKeys = []string{
		"a#b", "a:b", ":", ":a", "a:", "it's", `a"b`, "a'b",
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
		for _, key := range mustRefuseKeys {
			for _, val := range hostileValues {
				if _, err := encodeKeyValue(key, val); err == nil {
					t.Errorf("key %q (value %q) was accepted; it must be refused", key, val)
				}
			}
		}
	})

	t.Run("round-trips", func(t *testing.T) {
		for _, key := range append(append([]string{}, mustQuoteKeys...), mustRoundTripKeys...) {
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
	return encodeAppSpecYAML(appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "probe", Module: "example.com/probe"},
		Seed: []appspec.AppSpecSeedEntity{{
			Entity: "notes",
			Rows:   []map[string]any{{key: val}},
		}},
	})
}

func TestPackKeyQuotingPreservesMappingStructure(t *testing.T) {
	for _, key := range mustQuoteKeys {
		broke := false
		for _, val := range hostileValues {
			if rawRowRoundTrips(key, val) != nil {
				broke = true
				break
			}
		}
		if !broke {
			t.Errorf("bare key %q unexpectedly preserved its mapping in both contexts", key)
		}
	}
	for _, key := range mustRoundTripKeys {
		for _, val := range hostileValues {
			if err := rawRowRoundTrips(key, val); err != nil {
				t.Errorf("bare key %q with value %q changed the mapping: %v", key, val, err)
			}
		}
	}
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
// Values with quotes round-trip as one flow-list item when escaped.
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
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "probe"},
		Entities: []framework.EntityDeclaration{{
			Name: "windows",
			Fields: []framework.FieldDeclaration{{
				Name:   "width",
				Type:   "enum",
				Values: mustRoundTripEnumValues,
			}},
		}},
	}
	yml, err := encodeAppSpecYAML(bp)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	// Pin the context: the silent merge needs two members on ONE line, which
	// only the flow-list emission provides. If the writer ever moves enum
	// values to one-item-per-line, this guard — and the hazard it pins —
	// moves with it.
	//
	// Checking for "values: [" alone does NOT establish that: a flow list
	// broken across lines contains it too, and the hazard needs a shared
	// line to exist at all. So find the line and require it to close.
	flowLine := ""
	for _, line := range strings.Split(yml, "\n") {
		if strings.Contains(line, "values: [") {
			flowLine = line
			break
		}
	}
	if flowLine == "" || !strings.Contains(flowLine, "]") {
		t.Errorf("enum values are not emitted as a single-line flow list; the merge hazard this test pins lives only there:\n%s", yml)
	}
	b, err := decodeAppSpecString(yml)
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
