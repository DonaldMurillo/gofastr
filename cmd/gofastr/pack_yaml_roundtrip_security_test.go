package main

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// The invariant, stated end-to-end rather than as a predicate: pack either
// REFUSES a key or emits a file core/yaml can read back — and it refuses
// only what it must.
//
// yamlKeyRejectReason's table checks that each branch fires for the right
// reason. This checks the property that motivates having branches at all,
// and it is the test that would have caught the quote bypass: `it's` passed
// every branch, and the emitted `it's: "It's #1"` was unreadable because the
// key's apostrophe desynced the comment scanner from the correctly-quoted
// value. No predicate-level assertion covers an interaction like that.
//
// The two lists below are the oracle, deliberately NOT derived from
// yamlKeyRejectReason. Asking the code under test what it should do makes
// the test agree with any mutation of it: a first version of this called
// yamlKeyRejectReason for the expectation and a guard that refused EVERY
// key passed. The expectation has to be stated independently or it is not
// an expectation.
var (
	// Must be refused: each breaks the decode→encode→decode round trip.
	mustRefuseKeys = []string{
		"", "a\nb", "a\rb", "a:b", "a#b", "a[b", "a]b", "a{b", "a}b",
		"- a", "a\tb", " a", "a ", `"status`, "'status", "it's", `a"b`,
		`"quoted"`, "'quoted'",
	}
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
		for _, key := range mustRefuseKeys {
			for _, val := range hostileValues {
				if _, err := encodeKeyValue(key, val); err == nil {
					t.Errorf("key %q (value %q) was accepted; it breaks the round trip", key, val)
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
				if _, err := coreyaml.Parse(out); err != nil {
					t.Errorf("key %q with value %q emitted a file core/yaml cannot read (%v):\n%s",
						key, val, err, out)
				}
			}
		}
	})
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
