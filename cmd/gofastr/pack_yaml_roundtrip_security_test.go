package main

import (
	"fmt"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
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
	}
	// Refused conservatively: these would in fact survive raw emission
	// today, but the guard rejects them anyway because the predicate is
	// simpler than the parser's real rule. Listed separately so the
	// comment above stays true of the list above it.
	mustRefuseConservatively = []string{"a\rb", "a#b", `a"b`}

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
